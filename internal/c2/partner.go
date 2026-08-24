// Package c2 is the client for C2's partner API — the machine-to-machine
// surface at {origin}/partner, a sibling of /api (which is for citizen
// sessions).
//
// Two capabilities live behind it and share everything but the path: partner
// notifications (this file's PostNotification) and the payment broker. Both use
// the same origin, the same client credentials, and the same consent gate, so
// they share one client rather than two half-clients that drift.
//
// The consent gate is the thing to understand before using this: C2 sends
// nothing to a citizen who has not accepted this application's terms, and says
// so with 403. That is an expected outcome, not a failure — see ErrNoConsent.
package c2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrNoConsent means the citizen holds no active consent for this
	// application, so C2 refused to contact them. Nothing was created or sent.
	// Do not retry until they re-consent — retrying is both useless and audited
	// on C2's side as a denied attempt against our client id.
	ErrNoConsent = errors.New("c2: citizen has not consented to this application")
	// ErrUnknownSubject means C2 has no citizen with that subject.
	ErrUnknownSubject = errors.New("c2: no citizen matches that subject")
	// ErrUnauthorized means our client credentials were rejected or the client
	// is inactive — a configuration problem, not a per-citizen one.
	ErrUnauthorized = errors.New("c2: client credentials rejected")
	// ErrRateLimited means C2 is shedding load from this source; back off.
	ErrRateLimited = errors.New("c2: rate limited")
	// ErrNotConfigured means no partner origin is set, so the client is inert.
	ErrNotConfigured = errors.New("c2: partner API is not configured")
)

// Client talks to C2's partner API.
type Client struct {
	origin     string // e.g. https://portal.example.gov/c2 — no trailing slash
	clientID   string
	secret     string
	http       *http.Client
	appBaseURL string // this app's public base, for links in notification bodies
}

// Config carries what the client needs. An empty Origin yields a client whose
// calls all return ErrNotConfigured, which is how the app runs with no C2.
type Config struct {
	Origin     string
	ClientID   string
	Secret     string
	AppBaseURL string
	Timeout    time.Duration
}

// New builds a partner-API client.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		origin:     strings.TrimRight(strings.TrimSpace(cfg.Origin), "/"),
		clientID:   cfg.ClientID,
		secret:     cfg.Secret,
		appBaseURL: strings.TrimRight(cfg.AppBaseURL, "/"),
		http:       &http.Client{Timeout: timeout},
	}
}

// Configured reports whether calls will actually reach C2.
func (c *Client) Configured() bool {
	return c != nil && c.origin != "" && c.clientID != "" && c.secret != ""
}

// AppBaseURL is this app's public base, for building links in message bodies.
func (c *Client) AppBaseURL() string { return c.appBaseURL }

// Notification is one message to one citizen. C2 always creates the in-app
// notification and fans out to email/SMS according to the citizen's own
// preferences — we do not choose the channels.
type Notification struct {
	// Subject is the citizen's OIDC `sub`, exactly as received in the id_token.
	// We never send an email address or a name; C2 resolves the person.
	Subject string `json:"sub"`
	Title   string `json:"subject"`
	Body    string `json:"body"`
	// ShortBody is used for SMS. Keep it inside one segment; C2 falls back to
	// Body when it is empty, which will usually be truncated or split.
	ShortBody string `json:"shortBody,omitempty"`
	Category  string `json:"category,omitempty"` // BUSINESS (default) or PROMOTIONAL
}

// NotificationResult reports what C2 dispatched beyond the in-app notification.
type NotificationResult struct {
	NotificationID string   `json:"notificationId"`
	Channels       []string `json:"channels"`
}

// PostNotification sends one notification to one citizen.
func (c *Client) PostNotification(ctx context.Context, n Notification) (*NotificationResult, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	var out NotificationResult
	if err := c.post(ctx, "/partner/notifications", n, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// post issues an authenticated JSON request and maps C2's status codes onto the
// sentinel errors above.
//
// Auth is HTTP Basic with the OIDC client credentials. C2 also supports
// private-key JWT, which is its recommendation and avoids a shared secret on the
// wire; Basic is used here because this app already holds a client secret for
// the OIDC code exchange and has no signing key or published JWKS. Moving to
// private_key_jwt is a contained change to this method plus key management.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(c.clientID+":"+c.secret)))

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("c2: %s: %w", path, err)
	}
	defer res.Body.Close()
	return decodeStatus(path, res, out)
}

// decodeStatus maps C2's status codes onto the sentinel errors and decodes the
// body on success. Shared by post and get so both surfaces agree on what a 403
// means — that distinction (no consent, not a failure) is the whole reason the
// sentinels exist.
func decodeStatus(path string, res *http.Response, out any) error {
	switch res.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		if out == nil {
			return nil
		}
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	case http.StatusForbidden:
		return ErrNoConsent
	case http.StatusNotFound:
		return ErrUnknownSubject
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity:
		// C2 rejected the request on its own terms — most often a total that
		// does not match the sum of the lines. Retrying the same body is futile.
		return ErrInvoiceRejected
	default:
		// Never echo the response body verbatim: it is remote content and may
		// carry detail we would rather not put in our logs.
		return fmt.Errorf("c2: %s: unexpected status %d", path, res.StatusCode)
	}
}
