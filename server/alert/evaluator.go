// Package alert evaluates agent system metrics against configured thresholds
// and fires webhooks when a breach is sustained longer than `sustain`.
package alert

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/technonext/chowkidar/server/store"
)

// Metric identifies which dimension breached.
type Metric string

const (
	MetricCPU  Metric = "cpu"
	MetricMem  Metric = "memory"
	MetricDisk Metric = "disk"

	// Container-scoped metrics. A non-empty Event.ContainerName names
	// the offending container.
	MetricCtrCPU  Metric = "container_cpu"
	MetricCtrMem  Metric = "container_memory"
	MetricCtrDown Metric = "container_down"

	// Endpoint monitoring. EndpointName + EndpointURL identify the target.
	MetricEndpointDown Metric = "endpoint_down"

	// TLS leaf cert is within the SSL warning window. Value carries days
	// remaining; Threshold is the configured window in days.
	MetricSslExpiring Metric = "ssl_expiring"

	// Host-side reachability — fires when the agent stops reporting.
	MetricAgentDown Metric = "agent_down"
)

// Settings source for tunables that are read live each tick. Implemented by
// store.Store so the evaluator doesn't own the KV format.
type Settings interface {
	AlertSustain() time.Duration
	AlertResendCooldown() time.Duration
}

// key uniquely identifies an incident stream. Container-scoped metrics
// add the container name so concurrent containers track independently.
type key struct {
	agentID   string
	container string
	metric    Metric
}

// state tracks an in-flight incident per (agent, metric). lastFired is set
// once the webhook is dispatched for a given incident; it is reset to zero
// when the breach resolves, so the next incident can fire fresh.
type state struct {
	breachSince time.Time
	lastFired   time.Time
}

type Event struct {
	AgentID       string    `json:"agent_id"`
	Hostname      string    `json:"hostname"`
	Metric        Metric    `json:"metric"`
	ContainerName string    `json:"container_name,omitempty"`
	EndpointName  string    `json:"endpoint_name,omitempty"`
	EndpointURL   string    `json:"endpoint_url,omitempty"`
	Value         float64   `json:"value"`
	Threshold     int       `json:"threshold"`
	SustainedFor  string    `json:"sustained_for"`
	Timestamp     time.Time `json:"timestamp"`

	// Phase describes the event's place in the incident lifecycle.
	//   "observed" — first tick the breach was seen (UI-only, no webhook)
	//   "fired"    — sustain window elapsed, webhook dispatched
	//   "resolved" — breach ended after firing; webhook dispatched
	Phase string `json:"phase"`

	// Resolved is kept for backward-compatibility with older clients.
	Resolved bool `json:"resolved"`
}

const (
	PhaseObserved = "observed"
	PhaseFired    = "fired"
	PhaseResolved = "resolved"
)

// Evaluator runs one per-server goroutine. `Tick` every interval reads the
// joined (rules + latest metrics) view from the store and updates in-memory
// incident state, firing webhooks where appropriate. Sustain + resend-cooldown
// windows are re-read from Settings on each tick so admin-panel edits take
// effect without a restart.
type Evaluator struct {
	st       *store.Store
	settings Settings
	poster   *Poster
	broker   *Broker
	interval time.Duration

	mu   sync.Mutex
	seen map[key]*state
}

func NewEvaluator(st *store.Store, settings Settings, poster *Poster, broker *Broker, interval time.Duration) *Evaluator {
	return &Evaluator{
		st:       st,
		settings: settings,
		poster:   poster,
		broker:   broker,
		interval: interval,
		seen:     map[key]*state{},
	}
}

// Run blocks until ctx is cancelled, ticking at interval.
func (e *Evaluator) Run(ctx context.Context) {
	t := time.NewTicker(e.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick()
		}
	}
}

func (e *Evaluator) tick() {
	rows, err := e.st.ListEvaluationRows()
	if err != nil {
		log.Printf("[alert] list: %v", err)
		return
	}

	now := time.Now()
	sustain := e.settings.AlertSustain()
	resend := e.settings.AlertResendCooldown()
	active := map[key]struct{}{}

	for _, row := range rows {
		// Agent reachability check. Independent of HasMetrics — an agent
		// that has never reported is treated as down once the rule is on.
		if row.Rule.AgentDownEnabled {
			down := row.LastSeen == nil || now.Sub(*row.LastSeen) > sustain
			var value float64
			if down {
				value = 1
			}
			k := key{agentID: row.AgentID, metric: MetricAgentDown}
			active[k] = struct{}{}
			e.evaluateBool(k, "", down, value, row, now, sustain, resend)
		}

		// Host (system) metrics — only when latest system_metrics is present.
		if row.HasMetrics {
			checks := []struct {
				metric    Metric
				enabled   bool
				value     float64
				threshold int
			}{
				{MetricCPU, row.Rule.CPUEnabled, row.CPUPercent, row.Rule.CPUThreshold},
				{MetricMem, row.Rule.MemEnabled, row.MemPercent, row.Rule.MemThreshold},
				{MetricDisk, row.Rule.DiskEnabled, row.DiskPercent, row.Rule.DiskThreshold},
			}
			for _, c := range checks {
				if !c.enabled {
					continue
				}
				k := key{agentID: row.AgentID, metric: c.metric}
				active[k] = struct{}{}
				e.evaluate(k, "", c.value, c.threshold, row, now, sustain, resend)
			}
		}

		// Container-scoped metrics. One state slot per (agent, container, metric).
		if row.Rule.CtrDownEnabled || row.Rule.CtrCPUEnabled || row.Rule.CtrMemEnabled {
			containers, err := e.st.GetLatestContainers(row.AgentID)
			if err != nil {
				log.Printf("[alert] containers %s: %v", row.AgentID, err)
				continue
			}
			for _, c := range containers {
				if row.Rule.CtrDownEnabled {
					// Treat anything not starting with "Up" or "running" as down.
					down := !strings.HasPrefix(c.Status, "Up") && !strings.HasPrefix(c.Status, "running")
					var value float64
					if down {
						value = 1
					}
					k := key{agentID: row.AgentID, container: c.Name, metric: MetricCtrDown}
					active[k] = struct{}{}
					e.evaluateBool(k, c.Name, down, value, row, now, sustain, resend)
				}
				if row.Rule.CtrCPUEnabled {
					mcore := c.CPUPercent * 10 // 100% = 1 core = 1000 mCore
					k := key{agentID: row.AgentID, container: c.Name, metric: MetricCtrCPU}
					active[k] = struct{}{}
					e.evaluate(k, c.Name, mcore, row.Rule.CtrCPUThresholdMCore, row, now, sustain, resend)
				}
				if row.Rule.CtrMemEnabled && c.MemLimitMB > 0 {
					pct := c.MemUsedMB / c.MemLimitMB * 100
					k := key{agentID: row.AgentID, container: c.Name, metric: MetricCtrMem}
					active[k] = struct{}{}
					e.evaluate(k, c.Name, pct, row.Rule.CtrMemThreshold, row, now, sustain, resend)
				}
			}
		}
	}

	// Drop state for (agent, metric) pairs that are no longer evaluated
	// (rule disabled, agent alerts off, or agent deleted). Stops stale
	// breachSince values from spuriously firing if the rule comes back.
	e.mu.Lock()
	for k := range e.seen {
		if _, ok := active[k]; !ok {
			delete(e.seen, k)
		}
	}
	e.mu.Unlock()
}

// evaluate handles numeric-threshold metrics (host CPU/mem/disk + container
// CPU/mem). containerName is "" for host-level events.
func (e *Evaluator) evaluate(k key, containerName string, value float64, threshold int, row store.EvaluationRow, now time.Time, sustain, resend time.Duration) {
	e.mu.Lock()
	s, ok := e.seen[k]
	if !ok {
		s = &state{}
		e.seen[k] = s
	}
	breaching := value > float64(threshold)
	phase := ""
	if breaching {
		firstTick := s.breachSince.IsZero()
		if firstTick {
			s.breachSince = now
		}
		switch {
		case firstTick:
			// Instant UI feedback — not a firing event, just observation.
			phase = PhaseObserved
		case s.lastFired.IsZero() && now.Sub(s.breachSince) >= sustain:
			s.lastFired = now
			phase = PhaseFired
		case !s.lastFired.IsZero() && resend > 0 && now.Sub(s.lastFired) >= resend:
			s.lastFired = now
			phase = PhaseFired
		}
	} else {
		if !s.lastFired.IsZero() {
			phase = PhaseResolved
		}
		s.breachSince = time.Time{}
		s.lastFired = time.Time{}
	}
	e.mu.Unlock()

	if phase == "" {
		return
	}
	evt := Event{
		AgentID:       row.AgentID,
		Hostname:      row.Hostname,
		Metric:        k.metric,
		ContainerName: containerName,
		Value:         value,
		Threshold:     threshold,
		SustainedFor:  sustain.String(),
		Timestamp:     now.UTC(),
		Phase:         phase,
		Resolved:      phase == PhaseResolved,
	}

	// Always broadcast to in-process subscribers (frontend toasts).
	if e.broker != nil {
		e.broker.Publish(evt)
	}

	// Webhooks only on fired / resolved. "Observed" is UI-only.
	if phase == PhaseObserved {
		return
	}
	if row.WebhookURL == "" || row.WebhookType == "" {
		log.Printf("[alert] %s %s=%.1f %s %d (no webhook)",
			row.Hostname, k.metric, value,
			map[string]string{PhaseResolved: "resolved <=", PhaseFired: ">"}[phase], threshold)
		return
	}
	go e.poster.Send(row.WebhookURL, row.WebhookType, evt)
}

// evaluateBool tracks boolean breach states (e.g. container down/up). value
// is reported in the event for tooltip rendering — 1 means breaching, 0 OK.
func (e *Evaluator) evaluateBool(k key, containerName string, breaching bool, value float64, row store.EvaluationRow, now time.Time, sustain, resend time.Duration) {
	e.mu.Lock()
	s, ok := e.seen[k]
	if !ok {
		s = &state{}
		e.seen[k] = s
	}
	phase := ""
	if breaching {
		firstTick := s.breachSince.IsZero()
		if firstTick {
			s.breachSince = now
		}
		switch {
		case firstTick:
			phase = PhaseObserved
		case s.lastFired.IsZero() && now.Sub(s.breachSince) >= sustain:
			s.lastFired = now
			phase = PhaseFired
		case !s.lastFired.IsZero() && resend > 0 && now.Sub(s.lastFired) >= resend:
			s.lastFired = now
			phase = PhaseFired
		}
	} else {
		if !s.lastFired.IsZero() {
			phase = PhaseResolved
		}
		s.breachSince = time.Time{}
		s.lastFired = time.Time{}
	}
	e.mu.Unlock()

	if phase == "" {
		return
	}
	evt := Event{
		AgentID:       row.AgentID,
		Hostname:      row.Hostname,
		Metric:        k.metric,
		ContainerName: containerName,
		Value:         value,
		Threshold:     0,
		SustainedFor:  sustain.String(),
		Timestamp:     now.UTC(),
		Phase:         phase,
		Resolved:      phase == PhaseResolved,
	}
	if e.broker != nil {
		e.broker.Publish(evt)
	}
	if phase == PhaseObserved {
		return
	}
	if row.WebhookURL == "" || row.WebhookType == "" {
		return
	}
	go e.poster.Send(row.WebhookURL, row.WebhookType, evt)
}
