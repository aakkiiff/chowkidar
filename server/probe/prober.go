// Package probe runs HTTP GET checks against every configured endpoint and
// records the result. Healthy = 2xx or 3xx response. Latency is wall-clock
// from request start to response body close.
package probe

import (
	"context"
	"database/sql"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/store"
)

const (
	requestTO          = 10 * time.Second
	sslWarnThresholdD  = 14 // days from NotAfter to consider a cert "expiring soon"
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
	// Last NotAfter we fired an SSL-expiring alert for, per endpoint. When the
	// stored value matches the current cert's NotAfter, we suppress repeat
	// fires; when it changes (renewal), the suppression resets and a new
	// approaching expiry will fire again.
	sslFired map[int64]time.Time
}

func New(st *store.Store, broker *alert.Broker, poster *alert.Poster) *Prober {
	return &Prober{
		st:       st,
		broker:   broker,
		poster:   poster,
		states:   map[int64]*bool{},
		sslFired: map[int64]time.Time{},
		client: &http.Client{
			Timeout: requestTO,
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

// cycle probes all endpoints concurrently with a small worker pool.
func (p *Prober) cycle(ctx context.Context) {
	endpoints, err := p.st.AllEndpoints()
	if err != nil {
		log.Printf("[probe] list: %v", err)
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
		}
	}
	p.mu.Unlock()

	const workers = 8
	jobs := make(chan store.Endpoint)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				p.probeOne(ctx, e)
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

func (p *Prober) probeOne(ctx context.Context, e store.Endpoint) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTO)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, e.URL, nil)
	if err != nil {
		p.record(e, time.Now(), 0, 0, false, err.Error(), nil)
		return
	}
	req.Header.Set("User-Agent", "chowkidar-probe/1")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		latency := int(time.Since(start) / time.Millisecond)
		p.record(e, start, 0, latency, false, err.Error(), nil)
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
	p.record(e, start, resp.StatusCode, latency, ok, errStr, notAfter)
}

func (p *Prober) record(e store.Endpoint, start time.Time, code, latency int, ok bool, errStr string, certNotAfter *time.Time) {
	if err := p.st.RecordProbe(store.EndpointProbe{
		EndpointID:   e.ID,
		ProbedAt:     start,
		StatusCode:   code,
		LatencyMS:    latency,
		OK:           ok,
		Error:        errStr,
		CertNotAfter: certNotAfter,
	}); err != nil {
		log.Printf("[probe] record %d: %v", e.ID, err)
	}

	rule, err := p.st.GetAlertRule(e.AgentID)
	if err != nil {
		return
	}
	// Endpoint up/down alerts.
	if rule.EndpointDownEnabled {
		p.detectTransition(e, start, ok, errStr)
	}
	// SSL near-expiry alerts.
	if rule.SslAlertEnabled && certNotAfter != nil {
		p.detectSslExpiring(e, start, *certNotAfter)
	}
}

// detectTransition fires breach/resolve when the up/down state flips for an
// endpoint with alerts enabled. First observation is silent — avoids a flood
// of "down" alerts on server startup.
func (p *Prober) detectTransition(e store.Endpoint, at time.Time, ok bool, errStr string) {
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

	hostname, webhookURL, webhookType := p.lookupAgent(e.AgentID)
	evt := alert.Event{
		AgentID:      e.AgentID,
		Hostname:     hostname,
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
	if webhookURL != "" && webhookType != "" {
		go p.poster.Send(webhookURL, webhookType, evt)
	}
}

// detectSslExpiring fires once when the leaf cert's NotAfter falls inside the
// warning window, and again as "resolved" when a renewal moves NotAfter back
// outside the window. Suppressed re-fires for the same NotAfter timestamp so
// repeated probes don't flood the channel.
func (p *Prober) detectSslExpiring(e store.Endpoint, at time.Time, notAfter time.Time) {
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
		p.emitSsl(e, at, notAfter, alert.PhaseFired)
	case !expiringSoon && !lastFiredFor.IsZero() && !lastFiredFor.Equal(notAfter):
		// Cert was renewed — emit resolved + clear suppression.
		p.mu.Lock()
		delete(p.sslFired, e.ID)
		p.mu.Unlock()
		p.emitSsl(e, at, notAfter, alert.PhaseResolved)
	}
}

func (p *Prober) emitSsl(e store.Endpoint, at, notAfter time.Time, phase string) {
	hostname, webhookURL, webhookType := p.lookupAgent(e.AgentID)
	daysLeft := int(notAfter.Sub(at) / (24 * time.Hour))
	evt := alert.Event{
		AgentID:      e.AgentID,
		Hostname:     hostname,
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
	if webhookURL != "" && webhookType != "" {
		go p.poster.Send(webhookURL, webhookType, evt)
	}
}

// lookupAgent returns hostname + the agent's configured webhook (URL + type)
// so endpoint events can be dispatched without re-deriving them in the
// alert package.
func (p *Prober) lookupAgent(agentID string) (hostname, webhookURL, webhookType string) {
	a, err := p.st.GetAgentWithMetrics(agentID)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[probe] agent lookup %s: %v", agentID, err)
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
