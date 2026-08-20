package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Every registered module must be constructible and self-describing — the admin
// form renders straight off this registry.
func TestEveryModuleIsConstructible(t *testing.T) {
	seen := map[Kind]bool{}
	for _, m := range Modules() {
		if seen[m.Kind] {
			t.Fatalf("duplicate module kind %s", m.Kind)
		}
		seen[m.Kind] = true

		if m.Name == "" || m.Summary == "" {
			t.Errorf("module %s needs a name and summary for the admin form", m.Kind)
		}
		p, err := New(m.Kind, nil)
		if err != nil {
			t.Fatalf("New(%s): %v", m.Kind, err)
		}
		if p.Kind() != m.Kind {
			t.Errorf("New(%s) returned a provider reporting %s", m.Kind, p.Kind())
		}
	}
	if !seen[DefaultKind] {
		t.Errorf("the default module %s must be registered", DefaultKind)
	}
}

// Secrets belong in the environment, never in a form field that lands in the DB.
func TestTwoWayModulesDeclareSecretsAsEnvOnly(t *testing.T) {
	for _, m := range Modules() {
		if !m.TwoWay {
			continue
		}
		if m.SecretEnv == "" {
			t.Errorf("two-way module %s must name the env var carrying its credentials", m.Kind)
		}
		for _, f := range m.Fields {
			if f.Label == "" {
				t.Errorf("module %s field %s needs a label", m.Kind, f.Key)
			}
		}
	}
}

func TestNewRejectsUnknownKind(t *testing.T) {
	if _, err := New(Kind("carrier-pigeon"), nil); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("want ErrUnknownKind, got %v", err)
	}
}

// One-way modules say so rather than returning an empty list that would read as
// "nothing is blocked on the city calendar".
func TestOneWayModulesReportNotSupported(t *testing.T) {
	for _, k := range []Kind{KindICS, KindNone} {
		p, err := New(k, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.BusyWindows(context.Background(), "fac-1", time.Now(), time.Now().Add(time.Hour))
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("%s: want ErrNotSupported, got %v", k, err)
		}
		if err := p.Publish(context.Background(), domain.Booking{}); err != nil {
			t.Errorf("%s: publish should be a no-op, got %v", k, err)
		}
	}
}

// A selected-but-unbuilt module must fail loudly on every call, so it can never
// silently swallow a booking that the city calendar never received.
func TestPendingModulesFailLoudly(t *testing.T) {
	for _, k := range []Kind{KindGoogle, KindMicrosoft} {
		p, err := New(k, map[string]string{"calendarId": "spaces@rivermont.ca"})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Publish(context.Background(), domain.Booking{}); !errors.Is(err, ErrNotConnected) {
			t.Errorf("%s publish: want ErrNotConnected, got %v", k, err)
		}
		if err := p.Withdraw(context.Background(), domain.Booking{}); !errors.Is(err, ErrNotConnected) {
			t.Errorf("%s withdraw: want ErrNotConnected, got %v", k, err)
		}
		if _, err := p.BusyWindows(context.Background(), "fac-1", time.Now(), time.Now()); !errors.Is(err, ErrNotConnected) {
			t.Errorf("%s busy: want ErrNotConnected, got %v", k, err)
		}
	}
}
