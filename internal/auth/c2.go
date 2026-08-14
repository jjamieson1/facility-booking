package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
)

// c2Client fetches profile data (name, email) from C2's identity admin API. C2
// releases only the `sub` claim over OIDC, so this fills in the rest by looking
// the identity up by subject. It authenticates once with service credentials and
// reuses the session cookie, re-logging in on a 401.
type c2Client struct {
	baseURL string
	user    string
	pass    string
	http    *http.Client
	mu      sync.Mutex // guards login (one refresh at a time)
}

// newC2Client returns a client, or nil when C2 lookup isn't configured.
func newC2Client(baseURL, user, pass string) *c2Client {
	if baseURL == "" || user == "" || pass == "" {
		return nil
	}
	jar, _ := cookiejar.New(nil)
	return &c2Client{baseURL: baseURL, user: user, pass: pass, http: &http.Client{Jar: jar}}
}

// profile is the subset of C2 identity data this app displays.
type profile struct {
	Name  string
	Email string
}

type c2Identity struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

type c2Email struct {
	EmailAddress string `json:"emailAddress"`
	IsPrimary    bool   `json:"isPrimary"`
	IsVerified   bool   `json:"isVerified"`
}

// Profile fetches the identity's name and primary email by subject. A best-effort
// call: on any failure it returns what it has (possibly empty) so login proceeds.
func (c *c2Client) Profile(ctx context.Context, subject string) profile {
	var p profile
	var idn c2Identity
	if err := c.getJSON(ctx, "/api/identities/"+subject, &idn); err != nil {
		return p
	}
	p.Name = strings.TrimSpace(idn.FirstName + " " + idn.LastName)

	var emails []c2Email
	if err := c.getJSON(ctx, "/api/identities/"+subject+"/emails", &emails); err == nil {
		p.Email = pickPrimaryEmail(emails)
	}
	return p
}

// pickPrimaryEmail prefers a verified primary, then any primary, then the first.
func pickPrimaryEmail(emails []c2Email) string {
	var primary, verifiedPrimary, first string
	for i, e := range emails {
		if i == 0 {
			first = e.EmailAddress
		}
		if e.IsPrimary {
			primary = e.EmailAddress
			if e.IsVerified {
				verifiedPrimary = e.EmailAddress
			}
		}
	}
	switch {
	case verifiedPrimary != "":
		return verifiedPrimary
	case primary != "":
		return primary
	default:
		return first
	}
}

// getJSON GETs a C2 API path into dst, logging in first if needed and retrying
// once after a fresh login on 401 (expired session).
func (c *c2Client) getJSON(ctx context.Context, path string, dst any) error {
	status, err := c.doGet(ctx, path, dst)
	if err == nil && status == http.StatusOK {
		return nil
	}
	if status == http.StatusUnauthorized {
		if err := c.login(ctx); err != nil {
			return err
		}
		if status, err = c.doGet(ctx, path, dst); err == nil && status == http.StatusOK {
			return nil
		}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("c2: GET %s: status %d", path, status)
}

func (c *c2Client) doGet(ctx context.Context, path string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(dst)
	}
	return resp.StatusCode, nil
}

// login authenticates with the service account, storing the session in the jar.
func (c *c2Client) login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, c.user, c.pass)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/auth/login", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("c2: service login failed: status %d", resp.StatusCode)
	}
	return nil
}
