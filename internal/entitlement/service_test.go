package entitlement

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.New(t)
	return db
}

func mkUser(t *testing.T, db *gorm.DB) domain.User {
	t.Helper()
	u := domain.User{Subject: uuid.NewString(), Email: "r@example.com", Role: domain.RoleResident}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// fakeProvider lets a test drive each of the three outcomes that matter.
type fakeProvider struct {
	evaluate func(ref string) (Result, error)
	enrol    func(in map[string]string) (Result, error)
	calls    int
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Types() []Type { return []Type{TypeResidency} }
func (f *fakeProvider) Describe(Type) Descriptor {
	return Descriptor{Type: TypeResidency, Provider: "fake", Version: "1"}
}
func (f *fakeProvider) Evaluate(_ context.Context, _ Type, ref string) (Result, error) {
	f.calls++
	return f.evaluate(ref)
}
func (f *fakeProvider) Enrol(_ context.Context, _ Type, in map[string]string) (Result, error) {
	return f.enrol(in)
}

func granted(validFor time.Duration) Result {
	until := time.Now().Add(validFor)
	return Result{Outcome: domain.EntitlementGranted, Category: "resident", Ref: "ref-1", ValidUntil: &until}
}

// AC: a returning holder is re-validated silently against the stored reference —
// they prove nothing again.
func TestResolveRevalidatesSilently(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	p := &fakeProvider{evaluate: func(string) (Result, error) { return granted(time.Hour), nil }}
	svc := NewService(db, auditlog.New("", ""), p)

	// Enrol once.
	p.enrol = func(map[string]string) (Result, error) { return granted(time.Hour), nil }
	if _, err := svc.Prove(context.Background(), u, TypeResidency, map[string]string{"address": "x"}); err != nil {
		t.Fatal(err)
	}

	set := svc.Resolve(context.Background(), u)
	if !set.IsResident() {
		t.Fatalf("want resident, got %+v", set)
	}
	if p.calls != 1 {
		t.Errorf("want one silent re-validation call, got %d", p.calls)
	}
	if set.Live[0].Stale {
		t.Error("a freshly confirmed determination should not be marked stale")
	}
}

// AC: an unreachable provider is NOT a denial. The last good determination is
// served while it is still valid — otherwise an outage reprices every resident
// booking.
func TestUnreachableProviderServesLastGood(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	p := &fakeProvider{
		enrol:    func(map[string]string) (Result, error) { return granted(time.Hour), nil },
		evaluate: func(string) (Result, error) { return Result{}, ErrUnreachable },
	}
	svc := NewService(db, auditlog.New("", ""), p)
	if _, err := svc.Prove(context.Background(), u, TypeResidency, map[string]string{"address": "x"}); err != nil {
		t.Fatal(err)
	}

	set := svc.Resolve(context.Background(), u)
	if !set.IsResident() {
		t.Fatalf("an outage must not drop a valid resident to normal rates: %+v", set)
	}
	if !set.Live[0].Stale {
		t.Error("a cached determination should be flagged stale")
	}
}

// ...but only while the cached determination is still valid. Once it has
// expired there is nothing usable, and normal rates apply with an explanation.
func TestUnreachableProviderWithExpiredCacheFallsBack(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	past := time.Now().Add(-time.Hour)
	db.Create(&domain.EntitlementDetermination{
		UserID: u.ID, Type: string(TypeResidency), Outcome: domain.EntitlementGranted,
		Category: "resident", Provider: "fake", Ref: "ref-1",
		EvaluatedAt: past.Add(-time.Hour), ValidUntil: &past,
	})
	p := &fakeProvider{evaluate: func(string) (Result, error) { return Result{}, ErrUnreachable }}
	svc := NewService(db, auditlog.New("", ""), p)

	set := svc.Resolve(context.Background(), u)
	if set.IsResident() {
		t.Fatal("an expired cached determination must not still grant residency")
	}
	if len(set.Notices) != 1 || set.Notices[0].Reason != ReasonUnavailable {
		t.Fatalf("want an 'unavailable' notice, got %+v", set.Notices)
	}
}

// A reference the provider no longer recognises sends the holder to the proving
// form — distinct from unavailable, because the remedy differs.
func TestUnknownRefNeedsProving(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	db.Create(&domain.EntitlementDetermination{
		UserID: u.ID, Type: string(TypeResidency), Outcome: domain.EntitlementGranted,
		Category: "resident", Provider: "fake", Ref: "stale-ref", EvaluatedAt: time.Now(),
	})
	p := &fakeProvider{evaluate: func(string) (Result, error) { return Result{}, ErrRefUnknown }}
	svc := NewService(db, auditlog.New("", ""), p)

	set := svc.Resolve(context.Background(), u)
	if set.IsResident() {
		t.Fatal("an unrecognised reference must not grant residency")
	}
	if len(set.Notices) != 1 || set.Notices[0].Reason != ReasonNeedsProving {
		t.Fatalf("want a 'needs proving' notice, got %+v", set.Notices)
	}
}

// Switching provider must invalidate every stored reference for that type,
// rather than silently reinterpreting one provider's ref as another's.
func TestDeterminationFromAnotherProviderIsNotHonoured(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	until := time.Now().Add(time.Hour)
	db.Create(&domain.EntitlementDetermination{
		UserID: u.ID, Type: string(TypeResidency), Outcome: domain.EntitlementGranted,
		Category: "resident", Provider: "old-provider", Ref: "ref-1",
		EvaluatedAt: time.Now(), ValidUntil: &until,
	})
	p := &fakeProvider{evaluate: func(string) (Result, error) {
		t.Error("the new provider must not be asked about another provider's ref")
		return granted(time.Hour), nil
	}}
	svc := NewService(db, auditlog.New("", ""), p)

	set := svc.Resolve(context.Background(), u)
	if set.IsResident() {
		t.Fatal("a ref issued by a different provider must not grant residency")
	}
	if len(set.Notices) != 1 || set.Notices[0].Reason != ReasonNeedsProving {
		t.Fatalf("want a 'needs proving' notice, got %+v", set.Notices)
	}
}

// Nothing on file is simply "not held" — not an error, and never a blocker.
func TestNoDeterminationIsNotHeld(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	svc := NewService(db, auditlog.New("", ""), &fakeProvider{})

	set := svc.Resolve(context.Background(), u)
	if set.IsResident() {
		t.Fatal("a user with no determination is not a resident")
	}
	if len(set.Notices) != 1 || set.Notices[0].Reason != ReasonNotHeld {
		t.Fatalf("want a 'not held' notice, got %+v", set.Notices)
	}
}

// A denial from the provider is recorded and does not grant the entitlement.
func TestProveRecordsDenial(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	p := &fakeProvider{enrol: func(map[string]string) (Result, error) {
		return Result{Outcome: domain.EntitlementDenied, Category: "non-resident"}, nil
	}}
	svc := NewService(db, auditlog.New("", ""), p)

	det, err := svc.Prove(context.Background(), u, TypeResidency, map[string]string{"address": "elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if det.Outcome != domain.EntitlementDenied {
		t.Fatalf("outcome = %s, want denied", det.Outcome)
	}
	var refreshed domain.User
	db.First(&refreshed, "id = ?", u.ID)
	if refreshed.IsResident {
		t.Error("a denial must not leave the cached resident flag set")
	}
}

// The resident flag is a cache of the determination, kept in step in both
// directions so existing callers (pricing, reporting split) stay correct.
func TestResidentFlagTracksDetermination(t *testing.T) {
	db := newDB(t)
	u := mkUser(t, db)
	p := &fakeProvider{enrol: func(map[string]string) (Result, error) { return granted(time.Hour), nil }}
	svc := NewService(db, auditlog.New("", ""), p)

	if _, err := svc.Prove(context.Background(), u, TypeResidency, map[string]string{"address": "x"}); err != nil {
		t.Fatal(err)
	}
	var after domain.User
	db.First(&after, "id = ?", u.ID)
	if !after.IsResident {
		t.Fatal("a granted determination should set the cached flag")
	}

	p.enrol = func(map[string]string) (Result, error) {
		return Result{Outcome: domain.EntitlementDenied, Category: "non-resident"}, nil
	}
	if _, err := svc.Prove(context.Background(), after, TypeResidency, map[string]string{"address": "y"}); err != nil {
		t.Fatal(err)
	}
	db.First(&after, "id = ?", u.ID)
	if after.IsResident {
		t.Fatal("a later denial should clear the cached flag")
	}
}

// Discretion is a property of the type, not of a screen: subsidy must never be
// announced to bystanders, residency may be.
func TestDisclosureIsPerType(t *testing.T) {
	if !TypeResidency.Public() {
		t.Error("residency should be displayable")
	}
	if TypeSubsidy.Public() {
		t.Error("fee assistance must never be displayed to bystanders")
	}
	if Type("something-new").Public() {
		t.Error("an unknown type must default to discreet")
	}
}

// The stacking order is a property of the type too — the fee engine applies
// reductions in this order, so resolution must be deterministic.
func TestLiveSetIsOrderedByStackOrder(t *testing.T) {
	res, _ := InfoFor(TypeResidency)
	sub, _ := InfoFor(TypeSubsidy)
	if res.Order >= sub.Order {
		t.Fatalf("resident discount must stack before subsidy (%d vs %d)", res.Order, sub.Order)
	}
}
