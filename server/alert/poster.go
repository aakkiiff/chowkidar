package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

)

const (
	maxRetries = 3
	baseDelay  = 2 * time.Second
	requestTO  = 8 * time.Second
)

// Supported webhook provider types. New providers get a new constant + a
// formatter registered in `formatters`.
const (
	TypeDiscord = "discord"
)

// Poster fires alert events to configured webhook URLs with bounded retries.
type Poster struct {
	client *http.Client
}

func NewPoster() *Poster {
	return &Poster{client: &http.Client{
		Timeout: requestTO,
	}}
}

// Send marshals evt using the formatter registered for `kind` and POSTs it.
// Retries on network errors and 5xx; aborts on 4xx (mis-config).
func (p *Poster) Send(url, kind string, evt Event) {
	format, ok := formatters[kind]
	if !ok {
		slog.Warn("unsupported webhook type", "type", kind, "hostname", evt.Hostname)
		return
	}
	body, err := json.Marshal(format(evt))
	if err != nil {
		slog.Error("alert marshal failed", "err", err)
		return
	}

	delay := baseDelay
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2
		}
		status, err := p.post(url, body)
		if err == nil {
			return
		}
		if status >= 400 && status < 500 {
			slog.Warn("webhook client error, giving up", "url", url, "status", status, "err", err)
			return
		}
		slog.Warn("webhook attempt failed", "url", url, "attempt", attempt+1, "err", err)
	}
}

func (p *Poster) post(url string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTO)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "chowkidar-alerts/1")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("server status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// formatters maps webhook type -> payload builder. Add a new provider by
// registering a new entry here.
var formatters = map[string]func(Event) any{
	TypeDiscord: discordPayload,
}

// fmtMCore renders a millicore value as either "750 mCore" or "1.50 Core".
func fmtMCore(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%.0f mCore", m)
	}
	return fmt.Sprintf("%.2f Core", m/1000)
}

func humanMetric(m Metric) string {
	switch m {
	case MetricCPU:
		return "CPU"
	case MetricMem:
		return "Memory"
	case MetricDisk:
		return "Disk"
	case MetricCtrCPU:
		return "Container CPU"
	case MetricCtrMem:
		return "Container memory"
	case MetricCtrDown:
		return "Container down"
	case MetricEndpointDown:
		return "Endpoint down"
	case MetricSslExpiring:
		return "SSL expiring"
	case MetricAgentDown:
		return "Agent down"
	}
	return string(m)
}

// Discord Incoming Webhook: colored embed + short content line for mobile
// previews. Resolved events get a green embed and a distinct title so the
// incident timeline is scannable in the channel.
// See https://discord.com/developers/docs/resources/webhook
func discordPayload(e Event) any {
	metric := humanMetric(e.Metric)
	subject := e.Hostname
	switch {
	case e.EndpointName != "":
		subject = fmt.Sprintf("%s · %s", e.Hostname, e.EndpointName)
	case e.ContainerName != "":
		subject = fmt.Sprintf("%s · %s", e.Hostname, e.ContainerName)
	}
	// Render values per metric kind: CPU mCore for containers, % otherwise,
	// boolean for "down" events.
	valStr := fmt.Sprintf("%.1f%%", e.Value)
	threshStr := fmt.Sprintf("%d%%", e.Threshold)
	if e.Metric == MetricCtrCPU {
		valStr = fmtMCore(e.Value)
		threshStr = fmtMCore(float64(e.Threshold))
	}

	var title, desc, content string
	var color int
	switch {
	case e.Metric == MetricAgentDown:
		if e.Resolved {
			title = fmt.Sprintf("%s — agent reporting again", subject)
			desc = fmt.Sprintf("**%s** has resumed sending heartbeats.", e.Hostname)
			content = fmt.Sprintf("✅ %s: agent reporting again", subject)
			color = 0x73BF69
		} else {
			title = fmt.Sprintf("%s — agent down", subject)
			desc = fmt.Sprintf("**%s** has not reported for more than the sustain window.", e.Hostname)
			content = fmt.Sprintf("🚨 %s: agent down", subject)
			color = 0xE0626A
		}
	case e.Metric == MetricEndpointDown:
		if e.Resolved {
			title = fmt.Sprintf("%s — endpoint healthy", subject)
			desc = fmt.Sprintf("`%s` is responding 2xx/3xx again.", e.EndpointURL)
			content = fmt.Sprintf("✅ %s: endpoint healthy", subject)
			color = 0x73BF69
		} else {
			title = fmt.Sprintf("%s — endpoint down", subject)
			desc = fmt.Sprintf("`%s` is not responding with a 2xx/3xx status.", e.EndpointURL)
			content = fmt.Sprintf("🚨 %s: endpoint down", subject)
			color = 0xE0626A
		}
	case e.Metric == MetricSslExpiring:
		days := int(e.Value)
		if e.Resolved {
			title = fmt.Sprintf("%s — SSL renewed", subject)
			desc = fmt.Sprintf("`%s` certificate now valid for %d days.", e.EndpointURL, days)
			content = fmt.Sprintf("✅ %s: SSL renewed (%d days)", subject, days)
			color = 0x73BF69
		} else {
			title = fmt.Sprintf("%s — SSL expiring soon", subject)
			desc = fmt.Sprintf("`%s` certificate expires in **%d days** (threshold %d).",
				e.EndpointURL, days, e.Threshold)
			content = fmt.Sprintf("⚠️ %s: SSL expires in %d days", subject, days)
			color = 0xF2CC0C
		}
	case e.Metric == MetricCtrDown:
		if e.Resolved {
			title = fmt.Sprintf("%s — running again", subject)
			desc = fmt.Sprintf("Container **%s** is back to a running state.", e.ContainerName)
			content = fmt.Sprintf("✅ %s: container running again", subject)
			color = 0x73BF69
		} else {
			title = fmt.Sprintf("%s — container not running", subject)
			desc = fmt.Sprintf("Container **%s** has not been running for %s.", e.ContainerName, e.SustainedFor)
			content = fmt.Sprintf("🚨 %s: container not running", subject)
			color = 0xE0626A
		}
	case e.Resolved:
		title = fmt.Sprintf("%s — %s resolved", subject, metric)
		desc = fmt.Sprintf("**%s** is back to **%s**, under the **%s** threshold.",
			metric, valStr, threshStr)
		content = fmt.Sprintf("✅ %s: %s resolved (%s ≤ %s)",
			subject, metric, valStr, threshStr)
		color = 0x73BF69
	default:
		title = fmt.Sprintf("%s — %s breach", subject, metric)
		desc = fmt.Sprintf("**%s** at **%s** exceeds threshold **%s**, sustained for %s.",
			metric, valStr, threshStr, e.SustainedFor)
		content = fmt.Sprintf("🚨 %s: %s %s > %s",
			subject, metric, valStr, threshStr)
		color = 0xE0626A
	}
	return map[string]any{
		"content": content,
		"embeds": []map[string]any{{
			"title":       title,
			"description": desc,
			"color":       color,
			"timestamp":   e.Timestamp.Format(time.RFC3339),
			"footer":      map[string]any{"text": "chowkidar"},
		}},
	}
}
