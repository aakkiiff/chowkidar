package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Shipper reads log lines from a Collector and POSTs them to the server
// as newline-delimited JSON. Batches on time + byte threshold for efficiency.
type Shipper struct {
	serverURL string
	token     string
	client    *http.Client
	batchMS   time.Duration
	batchBuf  int
}

func NewShipper(serverURL, token string, batchMS time.Duration, batchBuf int) *Shipper {
	return &Shipper{
		serverURL: serverURL,
		token:     token,
		client: &http.Client{
			// Bound a single POST so a hung server (no FIN/RST) can't pin the
			// shipper forever and let the collector channel run drop-oldest
			// indefinitely.
			Timeout: 30 * time.Second,
		},
		batchMS:  batchMS,
		batchBuf: batchBuf,
	}
}

// Run reconnects with exponential backoff on network failure, but fails fast
// on HTTP 4xx (invalid token, bad body) — retrying those would just burn cycles.
func (s *Shipper) Run(ctx context.Context, src <-chan Line) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.ship(ctx, src)
		if err == nil || ctx.Err() != nil {
			return
		}
		if isClientError(err) {
			log.Printf("[logs] ship aborted: %v", err)
			return
		}
		log.Printf("[logs] ship: %v — retry in %v", err, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// ship sends one batch on each flush tick until src closes or an error occurs.
func (s *Shipper) ship(ctx context.Context, src <-chan Line) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	flush := time.NewTicker(s.batchMS)
	defer flush.Stop()

	doPost := func() error {
		if buf.Len() == 0 {
			return nil
		}
		body := make([]byte, buf.Len())
		copy(body, buf.Bytes())
		buf.Reset()
		return s.post(ctx, body)
	}

	for {
		select {
		case <-ctx.Done():
			return doPost()
		case ln, ok := <-src:
			if !ok {
				return doPost()
			}
			if err := enc.Encode(ln); err != nil {
				log.Printf("[logs] encode: %v", err)
				continue
			}
			if buf.Len() >= s.batchBuf {
				if err := doPost(); err != nil {
					return err
				}
			}
		case <-flush.C:
			if err := doPost(); err != nil {
				return err
			}
		}
	}
}

func (s *Shipper) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, s.serverURL+"/api/v1/logs/ingest", bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return &httpError{code: resp.StatusCode, body: string(b)}
	}
	return nil
}

func (s *Shipper) Close() { s.client.CloseIdleConnections() }

// httpError marks responses that came back as HTTP errors (not network failures).
type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string { return fmt.Sprintf("server %d: %s", e.code, e.body) }

// isClientError flags responses that would just keep failing on retry.
// 401 is treated as retryable: the server may have restarted with the same
// secret, or a token rotation is in flight; backoff is cheap. 408 / 429 are
// also retryable. Everything else in 4xx (400 bad body, 403 forbidden, 404
// route gone, etc.) is fatal — burning cycles won't fix it.
func isClientError(err error) bool {
	he, ok := err.(*httpError)
	if !ok {
		return false
	}
	if he.code < 400 || he.code >= 500 {
		return false
	}
	switch he.code {
	case http.StatusUnauthorized, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return true
}
