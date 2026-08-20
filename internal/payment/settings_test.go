package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newSettings(t *testing.T) (*SettingsService, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	return NewSettingsService(db, auditlog.New("", "")), db
}

func mkAdmin(t *testing.T, db *gorm.DB) domain.User {
	t.Helper()
	u := domain.User{Subject: uuid.NewString(), Email: "admin@rivermont.ca", Role: domain.RoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// With nothing configured the app charges through the simulated gateway, so a
// fresh install cannot accidentally attempt real payments.
func TestDefaultsToTheSimulatedGateway(t *testing.T) {
	svc, _ := newSettings(t)

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Selected != KindMock || set.Effective != KindMock {
		t.Fatalf("want mock/mock, got %s/%s", set.Selected, set.Effective)
	}
	if !set.Connected {
		t.Error("the default module should report as connected")
	}
}

// Selecting an unbuilt module records the decision but must not start taking
// real payments through it — and must say so.
func TestSelectingUnbuiltModuleFallsBackAndWarns(t *testing.T) {
	svc, db := newSettings(t)
	admin := mkAdmin(t, db)

	set, err := svc.Set(context.Background(), KindStripe, map[string]string{"publishableKey": "pk_test_123"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if set.Selected != KindStripe {
		t.Errorf("selection = %s, want stripe", set.Selected)
	}
	if set.Effective != KindMock {
		t.Errorf("effective = %s, want the mock fallback", set.Effective)
	}
	if set.Connected {
		t.Error("an unbuilt module must not report as connected")
	}
	if set.FallbackNotes == "" {
		t.Error("an operator must be told real payments are not being taken")
	}
	// The app charges through the working module, not a broken stub.
	if got := svc.Provider(context.Background()).Name(); got != string(KindMock) {
		t.Errorf("Provider() = %s, want the effective module", got)
	}
}

// A module that is registered but not implemented fails loudly if it is ever
// reached directly — silence here would confirm bookings nobody paid for.
func TestPendingProviderFailsLoudly(t *testing.T) {
	for _, k := range []Kind{KindStripe, KindMoneris} {
		p, err := New(k, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Charge(context.Background(), 1000, "4242424242424242"); !errors.Is(err, ErrNotConnected) {
			t.Errorf("%s charge: want ErrNotConnected, got %v", k, err)
		}
		if _, err := p.Refund(context.Background(), "ref"); !errors.Is(err, ErrNotConnected) {
			t.Errorf("%s refund: want ErrNotConnected, got %v", k, err)
		}
	}
}

func TestSetRejectsBadInput(t *testing.T) {
	svc, db := newSettings(t)
	admin := mkAdmin(t, db)

	cases := []struct {
		name string
		kind Kind
		cfg  map[string]string
		want error
	}{
		{"unknown module", Kind("bitcoin"), nil, ErrUnknownKind},
		{"unknown field", KindStripe, map[string]string{"publishableKey": "pk", "nope": "x"}, ErrUnknownField},
		{"missing required field", KindStripe, map[string]string{}, ErrMissingField},
		{"blank required field", KindStripe, map[string]string{"publishableKey": "  "}, ErrMissingField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Set(context.Background(), tc.kind, tc.cfg, admin); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// Secrets must never be storable through the form: the API key field is not a
// declared field, so posting one is rejected rather than persisted.
func TestSecretsCannotBeStoredThroughTheForm(t *testing.T) {
	svc, db := newSettings(t)
	admin := mkAdmin(t, db)

	for _, key := range []string{"secretKey", "apiKey", "FB_STRIPE_SECRET_KEY", "sk_live"} {
		cfg := map[string]string{"publishableKey": "pk_test_123", key: "sk_live_dangerous"}
		if _, err := svc.Set(context.Background(), KindStripe, cfg, admin); !errors.Is(err, ErrUnknownField) {
			t.Errorf("config key %q was accepted; secrets must come from the environment", key)
		}
	}
	// And the declared module names the env var instead.
	mod, _ := ModuleFor(KindStripe)
	if mod.SecretEnv == "" {
		t.Error("a module taking real payments must name the env var carrying its key")
	}
}

func TestSetIsSingletonAndAudited(t *testing.T) {
	svc, db := newSettings(t)
	admin := mkAdmin(t, db)

	for _, k := range []Kind{KindMock, KindStripe, KindMock} {
		cfg := map[string]string{}
		if k == KindStripe {
			cfg["publishableKey"] = "pk_test_123"
		}
		if _, err := svc.Set(context.Background(), k, cfg, admin); err != nil {
			t.Fatal(err)
		}
	}

	var rows int64
	db.Model(&domain.PaymentIntegration{}).Count(&rows)
	if rows != 1 {
		t.Errorf("want exactly 1 settings row, got %d", rows)
	}
	var audits int64
	db.Model(&domain.AuditLog{}).Where("action = ?", "payment.settings.update").Count(&audits)
	if audits != 3 {
		t.Errorf("want 3 audit rows, got %d", audits)
	}
}

// A module the build no longer registers must not take payments down.
func TestUnknownStoredKindFallsBack(t *testing.T) {
	svc, db := newSettings(t)
	db.Create(&domain.PaymentIntegration{
		Base: domain.Base{ID: domain.PaymentIntegrationID}, Kind: "retired-gateway",
	})

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Effective != DefaultKind {
		t.Fatalf("effective = %s, want the default", set.Effective)
	}
}

// Refunds must go back through the gateway that took the money. Switching
// modules after a payment must not refund through the new one.
func TestRefundRefusesAProviderMismatch(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db, Fixed(NewMockProvider()))

	u := domain.User{Subject: uuid.NewString(), Email: "r@example.com", Role: domain.RoleResident}
	db.Create(&u)
	f := domain.Facility{Name: "Hall", FeeCents: 5000}
	db.Create(&f)
	b := domain.Booking{FacilityID: f.ID, UserID: u.ID, Status: domain.StatusConfirmed, FeeCents: 5000}
	db.Create(&b)
	if _, err := svc.Pay(context.Background(), b.ID, SuccessCard); err != nil {
		t.Fatal(err)
	}

	// The municipality switches gateway; the old charge still lives with the old one.
	switched := NewService(db, Fixed(pendingProvider{kind: KindStripe}))
	if _, err := switched.Refund(context.Background(), b.ID); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("want ErrProviderMismatch, got %v", err)
	}
}
