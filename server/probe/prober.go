// Package probe runs HTTP GET checks against every configured endpoint and
// records the result. Healthy = 2xx or 3xx response. Latency is wall-clock
// from request start to response body close.
package probe

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/safedial"
	"github.com/technonext/chowkidar/server/store"
)

const (
	requestTO         = 10 * time.Second
	sslWarnThresholdD = 14 // days from NotAfter to consider a cert "expiring soon"
)

type Prober struct {
	st     *store.Store
	client *http.Client
	broker *alert.Broker
	poster *alert.Poster

	// states[endpointID] = last known healthy flag. Pointer-bool so we can
	// distinguish "never observed" (nil) from "currently healthy" (true).
	mu     sync.Mutex
	states map[int64]*bool
	// openIncident[endpointID] = id of the incident row currently held open
	// for this endpoint. 0 = no open incident. Refreshed lazily — first probe
	// after server restart picks up any leftover row via LatestOpenIncident.
	openIncident map[int64]int64
	// Last NotAfter we fired an SSL-expiring alert for, per endpoint. When the
	// stored value matches the current cert's NotAfter, we suppress repeat
	// fires; when it changes (renewal), the suppression resets and a new
	// approaching expiry will fire again.
	sslFired map[int64]time.Time
}

func New(st *store.Store, broker *alert.Broker, poster *alert.Poster) *Prober {
	return &Prober{
		st:           st,
		broker:       broker,
		poster:       poster,
		states:       map[int64]*bool{},
		openIncident: map[int64]int64{},
		sslFired:     map[int64]time.Time{},
		client: &http.Client{
			Transport: safedial.Transport(),
			Timeout:   requestTO,
			// Don't auto-follow redirects: many uptime monitors care about
			// the literal first response. 3xx is treated as healthy below.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Run probes every endpoint at the configured interval until ctx is cancelled.
// Interval is re-read each cycle so admin-panel changes take effect without
// a restart.
func (p *Prober) Run(ctx context.Context) {
	for {
		interval := p.st.ProbeInterval()
		if interval <= 0 {
			interval = 60 * time.Second
		}
		next := time.After(interval)
		p.cycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-next:
		}
	}
}

// agentInfo bundles the per-agent data needed for alert dispatch, built once
// per cycle to avoid repeated DB lookups inside the concurrent probe workers.
type agentInfo struct {
	hostname    string
	webhookURL  string
	webhookType string
	rule        store.AlertRule
}

// cycle probes all endpoints concurrently with a small worker pool.
func (p *Prober) cycle(ctx context.Context) {
	endpoints, err := p.st.AllEndpoints()
	if err != nil {
		slog.Warn("probe list failed", "err", err)
		return
	}
	if len(endpoints) == 0 {
		return
	}

	// Drop tracked state for endpoints that no longer exist (deleted).
	live := map[int64]struct{}{}
	for _, e := range endpoints {
		live[e.ID] = struct{}{}
	}
	p.mu.Lock()
	for id := range p.states {
		if _, ok := live[id]; !ok {
			delete(p.states, id)
			delete(p.sslFired, id)
			delete(p.openIncident, id)
		}
	}
	p.mu.Unlock()

	// Build per-cycle agent cache: 3 queries total regardless of endpoint count.
	cache := p.buildAgentCache()

	const workers = 8
	jobs := make(chan store.Endpoint)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				p.probeOne(ctx, e, cache)
			}
		}()
	}
	for _, e := range endpoints {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- e:
		}
	}
	close(jobs)
	wg.Wait()
}

// buildAgentCache fetches hostnames, alert rules, and webhooks in 3 queries
// and returns a map keyed by agentID.
func (p *Prober) buildAgentCache() map[string]agentInfo {
	hostnames, err := p.st.ListAgentHostnames()
	if err != nil {
		slog.Warn("probe cache hostnames failed", "err", err)
		hostnames = map[string]string{}
	}
	rules, err := p.st.ListAlertRules()
	if err != nil {
		slog.Warn("probe cache rules failed", "err", err)
		rules = map[string]store.AlertRule{}
	}
	webhooks, err := p.st.ListWebhooks()
	if err != nil {
		slog.Warn("probe cache webhooks failed", "err", err)
	}
	webhookByID := map[int64]store.Webhook{}
	for _, w := range webhooks {
		webhookByID[w.ID] = w
	}

	cache := map[string]agentInfo{}
	for id, hostname := range hostnames {
		rule := rules[id]
		info := agentInfo{hostname: hostname, rule: rule}
		if rule.WebhookID != nil {
			if w, ok := webhookByID[*rule.WebhookID]; ok {
				info.webhookURL = w.URL
				info.webhookType = w.Type
			}
		}
		cache[id] = info
	}
	return cache
}

func (p *Prober) probeOne(ctx context.Context, e store.Endpoint, cache map[string]agentInfo) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTO)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, e.URL, nil)
	if err != nil {
		p.record(e, time.Now(), 0, 0, false, err.Error(), nil, cache)
		return
	}
	req.Header.Set("User-Agent", "chowkidar-probe/1")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		latency := int(time.Since(start) / time.Millisecond)
		p.record(e, start, 0, latency, false, err.Error(), nil, cache)
		return
	}
	io.CopyN(io.Discard, resp.Body, 64*1024)
	resp.Body.Close()
	latency := int(time.Since(start) / time.Millisecond)

	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	errStr := ""
	if !ok {
		errStr = resp.Status
	}

	// Capture leaf certificate's NotAfter from the TLS handshake. nil for
	// plain http or when the handshake failed (already returned above).
	var notAfter *time.Time
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		na := resp.TLS.PeerCertificates[0].NotAfter
		notAfter = &na
	}
	p.record(e, start, resp.StatusCode, latency, ok, errStr, notAfter, cache)
}

func (p *Prober) record(e store.Endpoint, start time.Time, code, latency int, ok bool, errStr string, certNotAfter *time.Time, cache map[string]agentInfo) {
	if err := p.st.RecordProbe(store.EndpointProbe{
		EndpointID:   e.ID,
		ProbedAt:     start,
		StatusCode:   code,
		LatencyMS:    latency,
		OK:           ok,
		Error:        errStr,
		CertNotAfter: certNotAfter,
	}); err != nil {
		slog.Warn("probe record failed", "endpoint", e.ID, "err", err)
	}

	// Always record the incident transition for storage / uptime maths,
	// regardless of whether alerts are enabled. Alerts only decide whether
	// to push to the broker + webhook; storage is unconditional so the
	// historical uptime view remains accurate even after toggling alerts off.
	p.recordIncidentTransition(e, start, ok, code, errStr)

	info := cache[e.AgentID]
	// Endpoint up/down alerts.
	if info.rule.EndpointDownEnabled {
		p.detectTransition(e, start, ok, errStr, info)
	}
	// SSL near-expiry alerts.
	if info.rule.SslAlertEnabled && certNotAfter != nil {
		p.detectSslExpiring(e, start, *certNotAfter, info)
	}
}

// recordIncidentTransition writes outage windows to endpoint_incidents:
// ok→fail opens a new row, fail→fail bumps the open row, fail→ok closes it.
// Idempotent across server restarts via LatestOpenIncident.
func (p *Prober) recordIncidentTransition(e store.Endpoint, at time.Time, ok bool, status int, errStr string) {
	p.mu.Lock()
	openID, knownOpen := p.openIncident[e.ID]
	p.mu.Unlock()

	// Lazy re-attach to a leftover open row from a prior process.
	if !knownOpen {
		if id, err := p.st.LatestOpenIncident(e.ID); err == nil {
			openID = id
			p.mu.Lock()
			p.openIncident[e.ID] = id
			p.mu.Unlock()
		} else if err != sql.ErrNoRows {
			slog.Warn("probe latest-open failed", "endpoint", e.ID, "err", err)
		}
	}

	switch {
	case !ok && openID == 0:
		// First fail — open a fresh row.
		id, err := p.st.OpenIncident(e.ID, at, status, errStr)
		if err != nil {
			slog.Warn("probe open incident failed", "endpoint", e.ID, "err", err)
			return
		}
		p.mu.Lock()
		p.openIncident[e.ID] = id
		p.mu.Unlock()
	case !ok && openID != 0:
		// Continued fail — bump the existing row.
		if err := p.st.BumpIncident(openID, status, errStr); err != nil {
			slog.Warn("probe bump incident failed", "incident", openID, "err", err)
		}
	case ok && openID != 0:
		// Recovery — close the row.
		if err := p.st.CloseIncident(e.ID, at); err != nil {
			slog.Warn("probe close incident failed", "endpoint", e.ID, "err", err)
		}
		p.mu.Lock()
		delete(p.openIncident, e.ID)
		p.mu.Unlock()
	}
}

// detectTransition fires breach/resolve when the up/down state flips for an
// endpoint with alerts enabled. First observation is silent — avoids a flood
// of "down" alerts on server startup.
func (p *Prober) detectTransition(e store.Endpoint, at time.Time, ok bool, errStr string, info agentInfo) {
	p.mu.Lock()
	prev := p.states[e.ID]
	current := ok
	p.states[e.ID] = &current
	p.mu.Unlock()

	if prev == nil {
		return // first observation; do not emit
	}
	if *prev == ok {
		return // no transition
	}

	evt := alert.Event{
		AgentID:      e.AgentID,
		Hostname:     info.hostname,
		Metric:       alert.MetricEndpointDown,
		EndpointName: e.Name,
		EndpointURL:  e.URL,
		Timestamp:    at.UTC(),
	}
	if ok {
		evt.Phase = alert.PhaseResolved
		evt.Resolved = true
	} else {
		evt.Phase = alert.PhaseFired
	}
	_ = errStr

	if p.broker != nil {
		p.broker.Publish(evt)
	}
	if info.webhookURL != "" && info.webhookType != "" {
		go p.poster.Send(info.webhookURL, info.webhookType, evt)
	}
}

// detectSslExpiring fires once when the leaf cert's NotAfter falls inside the
// warning window, and again as "resolved" when a renewal moves NotAfter back
// outside the window. Suppressed re-fires for the same NotAfter timestamp so
// repeated probes don't flood the channel.
func (p *Prober) detectSslExpiring(e store.Endpoint, at time.Time, notAfter time.Time, info agentInfo) {
	threshold := time.Duration(sslWarnThresholdD) * 24 * time.Hour
	expiringSoon := notAfter.Sub(at) <= threshold

	p.mu.Lock()
	lastFiredFor := p.sslFired[e.ID]
	p.mu.Unlock()

	switch {
	case expiringSoon && lastFiredFor.Equal(notAfter):
		// Already fired for this exact NotAfter; suppress.
		return
	case expiringSoon:
		// New incident — record + emit.
		p.mu.Lock()
		p.sslFired[e.ID] = notAfter
		p.mu.Unlock()
		p.emitSsl(e, at, notAfter, alert.PhaseFired, info)
	case !expiringSoon && !lastFiredFor.IsZero() && !lastFiredFor.Equal(notAfter):
		// Cert was renewed — emit resolved + clear suppression.
		p.mu.Lock()
		delete(p.sslFired, e.ID)
		p.mu.Unlock()
		p.emitSsl(e, at, notAfter, alert.PhaseResolved, info)
	}
}

func (p *Prober) emitSsl(e store.Endpoint, at, notAfter time.Time, phase string, info agentInfo) {
	daysLeft := int(notAfter.Sub(at) / (24 * time.Hour))
	evt := alert.Event{
		AgentID:      e.AgentID,
		Hostname:     info.hostname,
		Metric:       alert.MetricSslExpiring,
		EndpointName: e.Name,
		EndpointURL:  e.URL,
		Value:        float64(daysLeft),
		Threshold:    sslWarnThresholdD,
		Phase:        phase,
		Resolved:     phase == alert.PhaseResolved,
		Timestamp:    at.UTC(),
	}
	if p.broker != nil {
		p.broker.Publish(evt)
	}
	if info.webhookURL != "" && info.webhookType != "" {
		go p.poster.Send(info.webhookURL, info.webhookType, evt)
	}
}

// lookupAgent returns hostname + the agent's configured webhook (URL + type)
// so endpoint events can be dispatched without re-deriving them in the
// alert package.
func (p *Prober) lookupAgent(agentID string) (hostname, webhookURL, webhookType string) {
	a, err := p.st.GetAgentWithMetrics(agentID)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Warn("probe agent lookup failed", "agent", agentID, "err", err)
		}
		return agentID, "", ""
	}
	hostname = a.Hostname

	rule, err := p.st.GetAlertRule(agentID)
	if err != nil || rule.WebhookID == nil {
		return hostname, "", ""
	}
	hooks, err := p.st.ListWebhooks()
	if err != nil {
		return hostname, "", ""
	}
	for _, w := range hooks {
		if w.ID == *rule.WebhookID {
			return hostname, w.URL, w.Type
		}
	}
	return hostname, "", ""
}
