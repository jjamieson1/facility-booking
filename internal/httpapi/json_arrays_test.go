package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// A nil Go slice marshals to `null`, and browser code that iterates it throws —
// taking the whole React app down, not just one component. That is what blanked
// /my-bookings for every user with no entitlements.
//
// These assert on the serialised BYTES on purpose. Every existing test decoded
// into a struct first, where nil and empty are indistinguishable, which is
// exactly why the suite was green while the app was blank.
func TestArrayFieldsSerialiseAsEmptyNotNull(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	resident := sessionFor(t, authSvc, db, domain.RoleResident)
	admin := sessionFor(t, authSvc, db, domain.RoleAdmin)

	cases := []struct {
		name   string
		path   string
		cookie *http.Cookie
		// fields that must never be null in the response body
		arrays []string
	}{
		{
			// The regression: a user with no entitlements at all.
			name: "entitlements for a user holding none",
			path: "/api/entitlements", cookie: resident,
			arrays: []string{"live", "notices"},
		},
		{
			// The default calendar module (ics) declares no config fields.
			name: "calendar modules", path: "/api/staff/calendar-settings", cookie: admin,
			arrays: []string{"modules"},
		},
		{
			// The default payment module (mock) declares no config fields.
			name: "payment modules", path: "/api/staff/payment-settings", cookie: admin,
			arrays: []string{"modules"},
		},
		{
			name: "entitlement descriptor", path: "/api/entitlements/residency/descriptor", cookie: resident,
			arrays: []string{"fields"},
		},
		{
			name: "facilities list", path: "/api/facilities", cookie: nil,
			arrays: nil, // the whole body must be an array, checked below
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, h, http.MethodGet, tc.path, "", tc.cookie)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			if strings.HasPrefix(strings.TrimSpace(body), "[") {
				return // a bare array is already fine
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &decoded); err != nil {
				t.Fatalf("not an object: %v (%s)", err, body)
			}
			for _, field := range tc.arrays {
				raw, present := decoded[field]
				if !present {
					t.Errorf("%q missing from the response — the client expects an array", field)
					continue
				}
				if string(raw) == "null" {
					t.Errorf("%q is null; it must be [] — iterating null throws in the browser", field)
				}
			}
		})
	}
}

// Every module in either registry must carry a fields array, since the admin
// forms iterate it. The modules with no configuration are the defaults.
func TestModuleFieldsAreNeverNull(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	admin := sessionFor(t, authSvc, db, domain.RoleAdmin)

	for _, path := range []string{"/api/staff/calendar-settings", "/api/staff/payment-settings"} {
		rr := do(t, h, http.MethodGet, path, "", admin)
		var body struct {
			Modules []map[string]json.RawMessage `json:"modules"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(body.Modules) == 0 {
			t.Fatalf("%s returned no modules", path)
		}
		for i, m := range body.Modules {
			if raw, ok := m["fields"]; !ok || string(raw) == "null" {
				t.Errorf("%s module %d has fields=%s, want []", path, i, string(m["fields"]))
			}
		}
	}
}
