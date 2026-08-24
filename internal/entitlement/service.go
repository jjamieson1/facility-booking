package entitlement

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Service resolves entitlements for a person, storing each determination with
// its provenance and expiry.
type Service struct {
	db        *gorm.DB
	audit     auditlog.Recorder
	providers map[Type]Provider
	now       func() time.Time
}

// NewService registers providers by the types they serve. A later provider
// registering the same type replaces the earlier one.
func NewService(db *gorm.DB, audit auditlog.Recorder, providers ...Provider) *Service {
	s := &Service{db: db, audit: audit, providers: map[Type]Provider{}, now: time.Now}
	for _, p := range providers {
		for _, t := range p.Types() {
			s.providers[t] = p
		}
	}
	return s
}

// ProviderFor returns the provider serving a type.
func (s *Service) ProviderFor(t Type) (Provider, bool) {
	p, ok := s.providers[t]
	return p, ok
}

// Describe returns the proving form's contract for a type.
func (s *Service) Describe(t Type) (Descriptor, error) {
	p, ok := s.providers[t]
	if !ok {
		return Descriptor{}, ErrUnsupportedType
	}
	d := p.Describe(t)
	// Non-nil so the proving form can iterate it without a null check; a nil
	// slice serialises as `null` and throws in the browser.
	if d.Fields == nil {
		d.Fields = []Field{}
	}
	return d, nil
}

// Resolve determines every registered entitlement for a user.
//
// It must be called **before** the booking transaction opens: that transaction
// holds row locks for double-booking prevention, and a provider callout inside
// it would hold them for the provider's latency. Types are resolved
// concurrently, because generalising multiplies the number of callouts.
func (s *Service) Resolve(ctx context.Context, user domain.User) Set {
	var (
		mu sync.Mutex
		// Initialised, not nil: a nil slice marshals to `null`, and a client
		// doing `set.live.find(...)` on null throws. That blanked /my-bookings
		// for every user with no entitlements — which, once residency became
		// provider-determined, is nearly everyone. Same convention as
		// RecurringResult and the facilities list.
		set = Set{Live: []Determination{}, Notices: []Notice{}}
		wg  sync.WaitGroup

		types = s.registeredTypes()
	)
	for _, t := range types {
		wg.Add(1)
		go func(t Type) {
			defer wg.Done()
			det, notice := s.resolveOne(ctx, user, t)
			mu.Lock()
			defer mu.Unlock()
			if det != nil {
				set.Live = append(set.Live, *det)
			}
			if notice != nil {
				set.Notices = append(set.Notices, *notice)
			}
		}(t)
	}
	wg.Wait()

	// Stable order so quotes, receipts and tests are deterministic; this is also
	// the order the fee engine stacks reductions in.
	sort.Slice(set.Live, func(i, j int) bool { return orderOf(set.Live[i].Type) < orderOf(set.Live[j].Type) })
	sort.Slice(set.Notices, func(i, j int) bool { return orderOf(set.Notices[i].Type) < orderOf(set.Notices[j].Type) })
	return set
}

func orderOf(t Type) int {
	if i, ok := types[t]; ok {
		return i.Order
	}
	return 1 << 30
}

func (s *Service) registeredTypes() []Type {
	out := make([]Type, 0, len(s.providers))
	for t := range s.providers {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return orderOf(out[i]) < orderOf(out[j]) })
	return out
}

// resolveOne handles a single type: silent re-validation against the stored
// reference, with the three-outcome handling of constraint 3.
func (s *Service) resolveOne(ctx context.Context, user domain.User, t Type) (*Determination, *Notice) {
	stored, found := s.latest(ctx, user.ID, t)
	if !found || stored.Ref == "" {
		return nil, &Notice{Type: t, Reason: ReasonNotHeld}
	}

	provider, ok := s.providers[t]
	if !ok {
		return nil, &Notice{Type: t, Reason: ReasonNotHeld}
	}

	// A determination from a provider we no longer use must not be honoured:
	// switching provider invalidates every reference for that type.
	if stored.Provider != provider.Name() {
		return nil, &Notice{Type: t, Reason: ReasonNeedsProving}
	}

	res, err := provider.Evaluate(ctx, t, stored.Ref)
	switch {
	case err == nil:
		saved := s.record(ctx, user, t, provider.Name(), res)
		if !saved.Live(s.now()) {
			return nil, &Notice{Type: t, Reason: ReasonNeedsProving}
		}
		return &Determination{
			Type: t, Category: saved.Category, Provider: saved.Provider, Ref: saved.Ref,
			EvaluatedAt: saved.EvaluatedAt, ValidUntil: saved.ValidUntil,
		}, nil

	case errors.Is(err, ErrUnreachable):
		// Constraint 3: unreachable is not a denial. Serve the last good
		// determination while it is still valid; only fall back to normal rates
		// when nothing usable is cached — otherwise a provider outage silently
		// reprices every resident booking.
		if stored.Live(s.now()) {
			return &Determination{
				Type: t, Category: stored.Category, Provider: stored.Provider, Ref: stored.Ref,
				EvaluatedAt: stored.EvaluatedAt, ValidUntil: stored.ValidUntil, Stale: true,
			}, nil
		}
		return nil, &Notice{Type: t, Reason: ReasonUnavailable}

	case errors.Is(err, ErrRefUnknown):
		return nil, &Notice{Type: t, Reason: ReasonNeedsProving}

	default:
		return nil, &Notice{Type: t, Reason: ReasonUnavailable}
	}
}

// Prove establishes a fresh determination from applicant-supplied inputs. The
// inputs are evidence *for* the provider; the decision is the provider's.
func (s *Service) Prove(ctx context.Context, user domain.User, t Type, inputs map[string]string) (*domain.EntitlementDetermination, error) {
	provider, ok := s.providers[t]
	if !ok {
		return nil, ErrUnsupportedType
	}
	res, err := provider.Enrol(ctx, t, inputs)
	if err != nil {
		return nil, err
	}
	saved := s.record(ctx, user, t, provider.Name(), res)
	return &saved, nil
}

// latest returns the most recent stored determination for a user and type.
func (s *Service) latest(ctx context.Context, userID string, t Type) (domain.EntitlementDetermination, bool) {
	var rows []domain.EntitlementDetermination
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND type = ?", userID, string(t)).
		Order("evaluated_at desc").Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return domain.EntitlementDetermination{}, false
	}
	return rows[0], true
}

// record persists a determination and audits it. Every determination that can
// affect a price is auditable — type, provider, ref, outcome. Never the
// evidence: the provider holds that.
func (s *Service) record(ctx context.Context, user domain.User, t Type, providerName string, res Result) domain.EntitlementDetermination {
	row := domain.EntitlementDetermination{
		UserID: user.ID, Type: string(t), Outcome: res.Outcome, Category: res.Category,
		Provider: providerName, Ref: res.Ref, EvaluatedAt: s.now(), ValidUntil: res.ValidUntil,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return row
	}
	if s.audit != nil {
		s.audit.Record(auditlog.Event{
			Action: "entitlement.determine", ActorID: user.ID, ActorEmail: user.Email,
			TargetType: "entitlement", TargetID: string(t),
			Message: string(res.Outcome) + " by " + providerName + " (ref " + res.Ref + ")",
		})
	}
	s.syncResidentFlag(ctx, user, t, row)
	return row
}

// syncResidentFlag keeps User.IsResident as a read-only cache of the current
// residency determination. Existing callers (the SPA badge, seed data, the
// reports split via Booking.Resident) keep working, but the flag is now written
// only here — never from a request body.
func (s *Service) syncResidentFlag(ctx context.Context, user domain.User, t Type, row domain.EntitlementDetermination) {
	if t != TypeResidency {
		return
	}
	live := row.Live(s.now())
	if user.IsResident == live {
		return
	}
	_ = s.db.WithContext(ctx).Model(&domain.User{}).
		Where("id = ?", user.ID).Update("is_resident", live).Error
}
