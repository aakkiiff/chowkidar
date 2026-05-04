package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/technonext/chowkidar/agent/types"
)

const (
	maxRetries = 3
	baseDelay  = time.Second
)

type Reporter struct {
	serverURL string
	token     string
	client    *http.Client
}

func NewReporter(serverURL, token string) *Reporter {
	return &Reporter{
		serverURL: serverURL,
		token:     token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send marshals the report and POSTs it. Retries up to maxRetries on network
// errors with exponential backoff. HTTP-level (4xx/5xx body) errors are not
// retried — the server received and rejected; burning cycles won't help.
// Honors ctx so a shutdown during a retry sleep returns immediately.
func (r *Reporter) Send(ctx context.Context, metrics *types.Report) error {
	data, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	delay := baseDelay
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("report retry", "attempt", attempt+1, "max", maxRetries+1, "delay", delay, "last_err", lastErr)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			delay *= 2
		}
		err = r.post(ctx, data)
		if err == nil {
			return nil
		}
		lastErr = err
		// 4xx/5xx with response body — don't retry, surface to caller.
		if isHTTPError(err) {
			return err
		}
	}
	return fmt.Errorf("after %d attempts: %w", maxRetries+1, lastErr)
}

func (r *Reporter) post(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, r.serverURL+"/api/v1/report", bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return err // network error — retryable
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &httpError{code: resp.StatusCode, body: string(body)}
	}
	return nil
}

func (r *Reporter) Close() {
	r.client.CloseIdleConnections()
}

// httpError marks errors that came back as HTTP responses (not network failures).
type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("server %d: %s", e.code, e.body)
}

func isHTTPError(err error) bool {
	_, ok := err.(*httpError)
	return ok
}
