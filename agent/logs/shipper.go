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
			// No overall timeout — POST body can live for seconds between batches.
			Timeout: 0,
		},
		batchMS:  batchMS,
		batchBuf: batchBuf,
	}
}

// Run reads lines from src and ships batches until ctx is cancelled.
// Reconnects with exponential backoff (capped) on network failure.
func (s *Shipper) Run(ctx context.Context, src <-chan Line) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := s.ship(ctx, src); err != nil && ctx.Err() == nil {
			log.Printf("[logs] ship: %v — retry in %v", err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
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
		return fmt.Errorf("server %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (s *Shipper) Close() { s.client.CloseIdleConnections() }
