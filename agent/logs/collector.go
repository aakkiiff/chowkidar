// Package logs streams stdout/stderr from running Docker containers and
// forwards batched lines to the Chowkidar server over a long-lived POST.
package logs

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// refreshInterval is how often the collector re-scans for new/gone containers.
const refreshInterval = 10 * time.Second

// perContainerBuffer is the max pending lines per container before drop-oldest
// kicks in. 512 × ~400B ≈ 200KB/container worst case.
const perContainerBuffer = 512

type Line struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Stream        string    `json:"stream"` // "stdout" or "stderr"
	Timestamp     time.Time `json:"timestamp"`
	Text          string    `json:"text"`
}

type Collector struct {
	cli     *client.Client
	out     chan Line
	dropped atomic.Uint64
	mu      sync.Mutex
	active  map[string]context.CancelFunc
}

func (c *Collector) Out() <-chan Line { return c.out }

func (c *Collector) Dropped() uint64 { return c.dropped.Load() }

// New wraps an existing Docker client. Callers should reuse one client across
// collectors to avoid multiple connections to the daemon.
func New(cli *client.Client) *Collector {
	return &Collector{
		cli:    cli,
		out:    make(chan Line, perContainerBuffer*4),
		active: make(map[string]context.CancelFunc),
	}
}

// Run scans containers periodically and spawns a tail goroutine for each one
// not already tracked. Returns when ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(refreshInterval)
	defer t.Stop()

	c.refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			c.stopAll()
			close(c.out)
			return
		case <-t.C:
			c.refresh(ctx)
		}
	}
}

func (c *Collector) refresh(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	containers, err := c.cli.ContainerList(listCtx, container.ListOptions{})
	if err != nil {
		log.Printf("[logs] list: %v", err)
		return
	}

	seen := make(map[string]struct{}, len(containers))
	for _, ct := range containers {
		seen[ct.ID] = struct{}{}
		c.mu.Lock()
		_, already := c.active[ct.ID]
		c.mu.Unlock()
		if already {
			continue
		}
		name := ""
		if len(ct.Names) > 0 {
			name = strings.TrimPrefix(ct.Names[0], "/")
		}
		c.startTail(ctx, ct.ID, name)
	}

	// Stop tails for containers that disappeared.
	c.mu.Lock()
	for id, cancel := range c.active {
		if _, ok := seen[id]; !ok {
			cancel()
			delete(c.active, id)
		}
	}
	c.mu.Unlock()
}

func (c *Collector) startTail(parent context.Context, id, name string) {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	c.active[id] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.active, id)
			c.mu.Unlock()
		}()
		if err := c.tail(ctx, id, name); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[logs] tail %s: %v", id[:12], err)
		}
	}()
}

// tail streams one container's logs until ctx is cancelled or the stream ends.
func (c *Collector) tail(ctx context.Context, id, name string) error {
	reader, err := c.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Tail:       "0", // only new lines from "now"
	})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Docker multiplexes stdout + stderr over a single stream with an 8-byte
	// header per frame: [stream(1)][0 0 0][length(4 BE)]. stream: 1=stdout 2=stderr.
	br := bufio.NewReader(reader)
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, header); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		streamByte := header[0]
		length := binary.BigEndian.Uint32(header[4:])
		if length == 0 {
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(br, payload); err != nil {
			return err
		}
		c.emit(id, name, streamByte, payload)
	}
}

// emit splits a multi-line payload on '\n', parses the Docker timestamp prefix,
// and pushes each line to the output channel, dropping the oldest on overflow.
func (c *Collector) emit(id, name string, streamByte byte, payload []byte) {
	streamName := "stdout"
	if streamByte == 2 {
		streamName = "stderr"
	}

	lines := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	for _, ln := range lines {
		ts, text := parseDockerLine(ln)
		l := Line{
			ContainerID:   id[:12],
			ContainerName: name,
			Stream:        streamName,
			Timestamp:     ts,
			Text:          text,
		}
		select {
		case c.out <- l:
		default:
			// Drop-oldest: pop one, then try once more.
			select {
			case <-c.out:
			default:
			}
			c.dropped.Add(1)
			select {
			case c.out <- l:
			default:
			}
		}
	}
}

// parseDockerLine splits Docker's "RFC3339Nano message" format.
func parseDockerLine(s string) (time.Time, string) {
	sp := strings.SplitN(s, " ", 2)
	if len(sp) != 2 {
		return time.Now().UTC(), s
	}
	t, err := time.Parse(time.RFC3339Nano, sp[0])
	if err != nil {
		return time.Now().UTC(), s
	}
	return t, sp[1]
}

func (c *Collector) stopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.active {
		cancel()
	}
	c.active = map[string]context.CancelFunc{}
}

