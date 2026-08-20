// Package auditlog mirrors staff audit events to the central audit-logging
// service (append-only, tamper-evident) over its REST API. Forwarding is
// best-effort and non-blocking: a failed audit hop never fails or slows the
// user's request — the local domain.AuditLog row remains the durable record.
package auditlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// appName identifies this service in the central audit log.
const appName = "facility-booking"

// Event is a single audit record to forward.
type Event struct {
	Action     string // e.g. "booking.approve"
	ActorID    string // the staff/admin user id
	ActorEmail string
	TargetType string // e.g. "booking"
	TargetID   string
	Message    string // human-readable summary
}

// Entry is a stored audit record read back from the central service.
type Entry struct {
	Index      uint64 `json:"index"`
	Timestamp  string `json:"timestamp"`
	Message    string `json:"message"`
	Action     string `json:"action"`
	ActorEmail string `json:"actorEmail"`
	ActorID    string `json:"actorId"`
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
}

// Recorder forwards audit events and reads them back. Record must not block the
// caller. Enabled reports whether a central service is configured.
type Recorder interface {
	Record(Event)
	List(ctx context.Context, limit int) ([]Entry, error)
	Enabled() bool
}

// noop discards events (used when no audit URL is configured).
type noop struct{}

func (noop) Record(Event)                               {}
func (noop) List(context.Context, int) ([]Entry, error) { return nil, nil }
func (noop) Enabled() bool                              { return false }

// forwarder POSTs events to {base}/v1/logs.
type forwarder struct {
	endpoint string
	token    string
	http     *http.Client
}

// New returns a Recorder. An empty baseURL yields a no-op recorder so the app
// runs without the audit service. token is optional (bearer auth).
func New(baseURL, token string) Recorder {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return noop{}
	}
	return &forwarder{
		endpoint: baseURL + "/v1/logs",
		token:    token,
		http:     &http.Client{Timeout: 5 * time.Second},
	}
}

type logRequest struct {
	App      string         `json:"app"`
	Level    string         `json:"level"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (f *forwarder) Enabled() bool { return true }

// Record forwards the event in the background so the request isn't slowed by the
// audit hop.
func (f *forwarder) Record(e Event) {
	go f.send(e)
}

// List reads the most recent facility-booking audit entries from the central
// service, newest first.
func (f *forwarder) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	url := fmt.Sprintf("%s?app=%s&limit=%d", f.endpoint, appName, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auditlog: list status %d", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			Index     uint64 `json:"index"`
			Timestamp string `json:"timestamp"`
			Record    struct {
				Message  string `json:"message"`
				Metadata struct {
					Action     string `json:"action"`
					ActorEmail string `json:"actorEmail"`
					ActorID    string `json:"actorId"`
					TargetType string `json:"targetType"`
					TargetID   string `json:"targetId"`
				} `json:"metadata"`
			} `json:"record"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, Entry{
			Index: it.Index, Timestamp: it.Timestamp, Message: it.Record.Message,
			Action: it.Record.Metadata.Action, ActorEmail: it.Record.Metadata.ActorEmail,
			ActorID: it.Record.Metadata.ActorID, TargetType: it.Record.Metadata.TargetType,
			TargetID: it.Record.Metadata.TargetID,
		})
	}
	// Newest first for display.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (f *forwarder) send(e Event) {
	body, err := json.Marshal(logRequest{
		App:     appName,
		Level:   "INFO",
		Message: e.Message,
		Metadata: map[string]any{
			"action":     e.Action,
			"actorId":    e.ActorID,
			"actorEmail": e.ActorEmail,
			"targetType": e.TargetType,
			"targetId":   e.TargetID,
		},
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		log.Printf("auditlog: forward failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("auditlog: forward returned status %d", resp.StatusCode)
	}
}
