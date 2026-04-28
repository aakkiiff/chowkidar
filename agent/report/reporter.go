package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// Send marshals the report and POSTs it to the server, retrying up to maxRetries
// times with exponential backoff on network errors.
func (r *Reporter) Send(metrics *types.Report) error {
	data, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	delay := baseDelay
	for attempt := range maxRetries + 1 {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2
		}
		if err = r.post(data); err == nil {
			return nil
		}
		// don't retry HTTP-level errors (4xx) — server received and rejected the request
		if isHTTPError(err) {
			return err
		}
	}
	return fmt.Errorf("after %d attempts: %w", maxRetries+1, err)
}

func (r *Reporter) post(data []byte) error {
	req, err := http.NewRequest(http.MethodPost, r.serverURL+"/api/v1/report", bytes.NewReader(data))
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
