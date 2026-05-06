package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/technonext/chowkidar/agent/types"
)

// maxConcurrent limits parallel Docker stat calls to protect the daemon.
// 100 was too aggressive — the daemon stalled on busy hosts with many
// containers, causing 8s per-call timeouts to expire. 20 keeps the daemon
// responsive while still scraping ~60 containers within the 10s tick.
const maxConcurrent = 20

type inspectCache struct {
	status       string
	restartCount int
	startedAt    string
}

type DockerCollector struct {
	cli      *client.Client
	cpuCores int
	mu       sync.Mutex
	cache    map[string]inspectCache // keyed by container ID
}

func NewDockerCollector() (*DockerCollector, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	return &DockerCollector{
		cli:      cli,
		cpuCores: runtime.NumCPU(),
		cache:    make(map[string]inspectCache),
	}, nil
}

// Client returns the underlying Docker client so other packages (e.g. logs)
// can share the single daemon connection instead of opening a second one.
func (d *DockerCollector) Client() *client.Client { return d.cli }

// Collect lists all containers and fetches their metrics concurrently.
func (d *DockerCollector) Collect() ([]types.ContainerMetrics, error) {
	listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer listCancel()

	containers, err := d.cli.ContainerList(listCtx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([]types.ContainerMetrics, 0, len(containers))
		sem     = make(chan struct{}, maxConcurrent)
	)

	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}

		wg.Add(1)
		go func(id, name, image, status string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// Per-container timeout: slow/zombie containers don't block others.
			// Includes wait time on the semaphore, so must cover queuing under load.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			m, err := d.collectOne(ctx, id, name, image, status)
			if err != nil {
				slog.Warn("container stats failed", "container", id[:12], "err", err)
				return
			}

			mu.Lock()
			results = append(results, m)
			mu.Unlock()
		}(c.ID, name, c.Image, c.State)
	}

	wg.Wait()

	// Evict cache entries for containers that no longer exist.
	alive := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		alive[c.ID] = struct{}{}
	}
	d.mu.Lock()
	for id := range d.cache {
		if _, ok := alive[id]; !ok {
			delete(d.cache, id)
		}
	}
	d.mu.Unlock()

	return results, nil
}

// collectOne fetches stats + inspect data for a single container.
func (d *DockerCollector) collectOne(ctx context.Context, id, name, image, status string) (types.ContainerMetrics, error) {
	stats, err := d.cli.ContainerStats(ctx, id, false)
	if err != nil {
		return types.ContainerMetrics{}, fmt.Errorf("stats: %w", err)
	}
	defer stats.Body.Close()

	s, err := decodeStats(stats.Body)
	if err != nil {
		return types.ContainerMetrics{}, fmt.Errorf("decode: %w", err)
	}

	// Sum network I/O across all interfaces.
	var netRx, netTx uint64
	for _, n := range s.Networks {
		netRx += n.RxBytes
		netTx += n.TxBytes
	}

	// Inspect gives restart count and precise start time.
	// Cache result keyed by container ID; re-inspect only when status changes.
	restartCount := 0
	startedAt := ""
	d.mu.Lock()
	cached, hit := d.cache[id]
	d.mu.Unlock()
	if hit && cached.status == status {
		restartCount = cached.restartCount
		startedAt = cached.startedAt
	} else {
		if info, err := d.cli.ContainerInspect(ctx, id); err == nil {
			restartCount = info.RestartCount
			if info.State != nil {
				startedAt = info.State.StartedAt
			}
		}
		d.mu.Lock()
		d.cache[id] = inspectCache{status: status, restartCount: restartCount, startedAt: startedAt}
		d.mu.Unlock()
	}

	return types.ContainerMetrics{
		Name:         cleanName(name),
		ID:           id[:12],
		Image:        getImageName(image),
		Status:       status,
		CPUPercent:   calculateCPU(s, d.cpuCores),
		MemUsedMB:    float64(s.MemoryStats.Usage) / 1024 / 1024,
		MemLimitMB:   float64(s.MemoryStats.Limit) / 1024 / 1024,
		RestartCount: restartCount,
		StartedAt:    startedAt,
		NetRxMB:      float64(netRx) / 1024 / 1024,
		NetTxMB:      float64(netTx) / 1024 / 1024,
	}, nil
}

// calculateCPU computes the container's CPU usage percentage.
func calculateCPU(s statsJSON, cores int) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)

	if systemDelta <= 0 || cpuDelta <= 0 {
		return 0
	}

	return (cpuDelta / systemDelta) * float64(cores) * 100
}

// statsJSON matches the subset of Docker's stats API response we need.
type statsJSON struct {
	CPUStats    cpuStats            `json:"cpu_stats"`
	PreCPUStats cpuStats            `json:"precpu_stats"`
	MemoryStats memStats            `json:"memory_stats"`
	Networks    map[string]netStats `json:"networks"`
}

type cpuStats struct {
	CPUUsage       cpuUsage `json:"cpu_usage"`
	SystemCPUUsage uint64   `json:"system_cpu_usage"`
}

type cpuUsage struct {
	TotalUsage uint64 `json:"total_usage"`
}

type memStats struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
}

type netStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

func decodeStats(r io.Reader) (statsJSON, error) {
	var s statsJSON
	return s, json.NewDecoder(r).Decode(&s)
}

func getImageName(image string) string {
	// Strip registry prefix (everything before last "/") but keep the tag.
	// e.g. "docker.io/library/nginx:1.25" → "nginx:1.25"
	// e.g. "sha256:abc123..." → "sha256:abc123..." (no slash, returned as-is)
	colonIdx := strings.Index(image, ":")
	slashIdx := strings.LastIndex(image, "/")
	if slashIdx == -1 || (colonIdx != -1 && colonIdx < slashIdx) {
		// no slash, or colon is before the last slash (digest case) — return as-is
		return image
	}
	return image[slashIdx+1:]
}

// cleanName strips the leading "/" Docker prepends to container names.
func cleanName(name string) string {
	return strings.TrimPrefix(name, "/")
}
