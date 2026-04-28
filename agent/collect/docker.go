package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/technonext/chowkidar/agent/types"
)

// maxConcurrent limits parallel Docker stat calls to protect the daemon.
const maxConcurrent = 10

type DockerCollector struct {
	cli      *client.Client
	cpuCores int
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
	}, nil
}

// Client returns the underlying Docker client so other packages (e.g. logs)
// can share the single daemon connection instead of opening a second one.
func (d *DockerCollector) Client() *client.Client { return d.cli }

// Collect lists all containers and fetches their metrics concurrently.
func (d *DockerCollector) Collect() ([]types.ContainerMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	containers, err := d.cli.ContainerList(ctx, container.ListOptions{All: true})
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

			m, err := d.collectOne(ctx, id, name, image, status)
			if err != nil {
				log.Printf("[docker] %s: %v", id[:12], err)
				return
			}

			mu.Lock()
			results = append(results, m)
			mu.Unlock()
		}(c.ID, name, c.Image, c.Status)
	}

	wg.Wait()
	return results, nil
}

// collectOne fetches stats for a single container.
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

	return types.ContainerMetrics{
		Name:       cleanName(name),
		ID:         id[:12],
		Image:      getImageName(image),
		Status:     status,
		CPUPercent: calculateCPU(s, d.cpuCores),
		MemUsedMB:  float64(s.MemoryStats.Usage) / 1024 / 1024,
		MemLimitMB: float64(s.MemoryStats.Limit) / 1024 / 1024,
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
	CPUStats    cpuStats `json:"cpu_stats"`
	PreCPUStats cpuStats `json:"precpu_stats"`
	MemoryStats memStats `json:"memory_stats"`
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

func decodeStats(r io.Reader) (statsJSON, error) {
	var s statsJSON
	return s, json.NewDecoder(r).Decode(&s)
}

func getImageName(image string) string {
	name := strings.Split(image, ":")[0]
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		return name[idx+1:]
	}
	return name
}

// cleanName strips the leading "/" Docker prepends to container names.
func cleanName(name string) string {
	return strings.TrimPrefix(name, "/")
}
