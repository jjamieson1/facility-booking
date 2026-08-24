package policy

import (
	"context"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// standard is the seeded municipal default: full refund a week out, half inside
// that, nothing within 48 hours.
func standard() domain.CancellationPolicy {
	return domain.CancellationPolicy{
		Name:                    "Municipal default",
		ModificationCutoffHours: 24,
		Tiers: []domain.RefundTier{
			{HoursBefore: 168, RefundPercent: 100},
			{HoursBefore: 48, RefundPercent: 50},
		},
	}
}

func bookingStartingIn(d time.Duration, now time.Time) domain.Booking {
	return domain.Booking{StartsAt: now.Add(d), EndsAt: now.Add(d + time.Hour), FeeCents: 10000}
}

// The four cases the ticket calls out: in-window, out-of-window, partial, and a
// booking with nothing paid.
func TestQuoteAcrossTiers(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		startsIn   time.Duration
		paid       int
		wantRefund int
		wantPct    int
	}{
		{"well inside the full-refund tier", 30 * 24 * time.Hour, 10000, 10000, 100},
		{"exactly on the full-refund boundary", 168 * time.Hour, 10000, 10000, 100},
		{"just inside the half-refund tier", 167 * time.Hour, 10000, 5000, 50},
		{"exactly on the half-refund boundary", 48 * time.Hour, 10000, 5000, 50},
		{"past the last tier — nothing back", 47 * time.Hour, 10000, 0, 0},
		{"hours before the start", 2 * time.Hour, 10000, 0, 0},
		{"after it has started", -time.Hour, 10000, 0, 0},
		{"free facility, nothing paid", 30 * 24 * time.Hour, 0, 0, 0},
		{"paid booking, odd cents, rounds in the resident's favour", 100 * time.Hour, 999, 500, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := Quote(standard(), bookingStartingIn(tc.startsIn, now), tc.paid, now)
			if q.RefundCents != tc.wantRefund {
				t.Errorf("refund = %d, want %d", q.RefundCents, tc.wantRefund)
			}
			if q.RefundPercent != tc.wantPct {
				t.Errorf("percent = %d, want %d", q.RefundPercent, tc.wantPct)
			}
			if q.Explanation == "" {
				t.Error("every quote must explain itself — the resident is shown this")
			}
		})
	}
}

// A non-refundable charge comes off the top and can never push a refund below
// zero or above what was paid.
func TestNonRefundablePortion(t *testing.T) {
	now := time.Now()
	p := standard()
	p.NonRefundableCents = 1500

	full := Quote(p, bookingStartingIn(30*24*time.Hour, now), 10000, now)
	if full.RefundCents != 8500 {
		t.Errorf("full-tier refund = %d, want 8500 (10000 less the 1500 charge)", full.RefundCents)
	}

	// 50% of 2000 is 1000, less a 1500 charge — floored at zero, not negative.
	small := Quote(p, bookingStartingIn(100*time.Hour, now), 2000, now)
	if small.RefundCents != 0 {
		t.Errorf("refund = %d, want 0 — a fee larger than the refund must not go negative", small.RefundCents)
	}
}

// A refund can never exceed what was actually paid, whatever the policy says.
func TestRefundNeverExceedsAmountPaid(t *testing.T) {
	now := time.Now()
	p := domain.CancellationPolicy{
		Name:  "Overgenerous",
		Tiers: []domain.RefundTier{{HoursBefore: 1, RefundPercent: 150}},
	}
	q := Quote(p, bookingStartingIn(48*time.Hour, now), 5000, now)
	if q.RefundCents != 5000 {
		t.Fatalf("refund = %d, want it capped at the 5000 paid", q.RefundCents)
	}
}

// A policy with no tiers refunds nothing, and says so rather than erroring.
func TestPolicyWithNoTiers(t *testing.T) {
	now := time.Now()
	q := Quote(domain.CancellationPolicy{Name: "No refunds"}, bookingStartingIn(90*24*time.Hour, now), 10000, now)
	if q.RefundCents != 0 {
		t.Errorf("refund = %d, want 0", q.RefundCents)
	}
	if q.Explanation == "" {
		t.Error("a non-refundable policy still owes the resident an explanation")
	}
}

// Resolution order: the facility's own policy wins over the municipal default,
// which wins over the built-in.
func TestForPrefersFacilityPolicyThenDefault(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	ctx := context.Background()

	fac := domain.Facility{Name: "Arena"}
	db.Create(&fac)
	other := domain.Facility{Name: "Meeting room"}
	db.Create(&other)

	// Nothing configured at all → the built-in.
	if p, err := svc.For(ctx, fac.ID); err != nil {
		t.Fatal(err)
	} else if p.Name != DefaultPolicy().Name {
		t.Errorf("with no rows, want the built-in default, got %q", p.Name)
	}

	// A municipality-wide default applies to every facility.
	muni := domain.CancellationPolicy{Name: "Municipal default", ModificationCutoffHours: 24}
	db.Create(&muni)
	if p, err := svc.For(ctx, fac.ID); err != nil {
		t.Fatal(err)
	} else if p.Name != "Municipal default" {
		t.Errorf("want the municipal default, got %q", p.Name)
	}

	// A facility's own policy overrides it — for that facility only.
	own := domain.CancellationPolicy{Name: "Arena terms", FacilityID: &fac.ID}
	db.Create(&own)
	if p, err := svc.For(ctx, fac.ID); err != nil {
		t.Fatal(err)
	} else if p.Name != "Arena terms" {
		t.Errorf("want the facility's own policy, got %q", p.Name)
	}
	if p, err := svc.For(ctx, other.ID); err != nil {
		t.Fatal(err)
	} else if p.Name != "Municipal default" {
		t.Errorf("another facility must keep the default, got %q", p.Name)
	}
}

func TestModificationCutoff(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	fac := domain.Facility{Name: "Hall"}
	db.Create(&fac)
	db.Create(&domain.CancellationPolicy{Name: "Municipal default", ModificationCutoffHours: 24})

	got, err := svc.ModificationCutoff(context.Background(), fac.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != 24*time.Hour {
		t.Fatalf("cutoff = %v, want 24h", got)
	}
}

// Tiers are evaluated most-generous-first regardless of the order they are
// stored in, so a policy edited in the back-office cannot silently invert.
func TestTierOrderDoesNotDependOnStorageOrder(t *testing.T) {
	now := time.Now()
	p := standard()
	p.Tiers = []domain.RefundTier{
		{HoursBefore: 48, RefundPercent: 50},
		{HoursBefore: 168, RefundPercent: 100},
	}
	if q := Quote(p, bookingStartingIn(200*time.Hour, now), 10000, now); q.RefundCents != 10000 {
		t.Fatalf("refund = %d, want the 100%% tier to win at 200h out", q.RefundCents)
	}
}
