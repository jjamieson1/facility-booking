// Package policy resolves a facility's cancellation terms and works out what a
// cancellation refunds (§4.7 "refunds follow the cancellation policy", §4.9).
//
// It deliberately knows nothing about payment gateways. It answers "how much is
// owed", and the caller issues it — which keeps the gateway call out of the
// booking transaction, the same separation the entitlement work needed.
package policy

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Service resolves policies and quotes refunds.
type Service struct{ db *gorm.DB }

// NewService constructs the policy service.
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// DefaultPolicy is used when the database holds no policy at all — neither for
// the facility nor a municipality-wide default.
//
// This is a real fallback rather than an error because a missing policy must
// never block someone from cancelling; the cost of guessing is a refund figure,
// not a broken booking. It is deliberately conservative and is seeded into the
// database (see internal/seed) so the operator can see and edit it rather than
// discovering it in code.
func DefaultPolicy() domain.CancellationPolicy {
	return domain.CancellationPolicy{
		Name:                    "Municipal default",
		ModificationCutoffHours: 24,
		Tiers: []domain.RefundTier{
			{HoursBefore: 168, RefundPercent: 100}, // 7 days or more: full refund
			{HoursBefore: 48, RefundPercent: 50},   // 2–7 days: half
		},
	}
}

// For returns the policy governing a facility: its own if it has one, otherwise
// the municipality-wide default, otherwise DefaultPolicy.
func (s *Service) For(ctx context.Context, facilityID string) (domain.CancellationPolicy, error) {
	var own []domain.CancellationPolicy
	if err := s.db.WithContext(ctx).Preload("Tiers").Limit(1).
		Find(&own, "facility_id = ?", facilityID).Error; err != nil {
		return domain.CancellationPolicy{}, err
	}
	if len(own) == 1 {
		return own[0], nil
	}

	var fallback []domain.CancellationPolicy
	if err := s.db.WithContext(ctx).Preload("Tiers").Limit(1).
		Find(&fallback, "facility_id IS NULL").Error; err != nil {
		return domain.CancellationPolicy{}, err
	}
	if len(fallback) == 1 {
		return fallback[0], nil
	}
	return DefaultPolicy(), nil
}

// Quote works out what cancelling this booking right now would refund.
//
// paidCents is what the booker actually paid, not the booking's fee: a booking
// that was never paid refunds nothing regardless of how generous the policy is,
// and a free facility is simply the same case with zero.
func Quote(p domain.CancellationPolicy, b domain.Booking, paidCents int, now time.Time) domain.Quote {
	hours := domain.HoursUntil(b.StartsAt, now)
	q := domain.Quote{
		PolicyName:      p.Name,
		PaidCents:       paidCents,
		HoursUntilStart: hours,
	}

	if paidCents <= 0 {
		q.Explanation = "Nothing was paid for this booking, so there is nothing to refund."
		return q
	}

	pct, tierHours, ok := tierFor(p, hours)
	q.RefundPercent, q.AppliedTierHours = pct, tierHours
	if !ok || pct <= 0 {
		q.Explanation = fmt.Sprintf(
			"Cancelling less than %s before the start is non-refundable under %q.",
			describeHours(smallestTierHours(p)), p.Name)
		return q
	}

	// Round half up so a 50%% refund of an odd number of cents favours the
	// resident rather than the municipality. Integer cents throughout — the
	// codebase never uses floats for money.
	refund := (paidCents*pct + 50) / 100
	refund -= p.NonRefundableCents
	if refund < 0 {
		refund = 0
	}
	if refund > paidCents {
		refund = paidCents
	}
	q.RefundCents = refund

	switch {
	case refund == 0:
		q.Explanation = fmt.Sprintf(
			"A %d%% refund applies, but the non-refundable charge leaves nothing to return under %q.",
			pct, p.Name)
	case p.NonRefundableCents > 0:
		q.Explanation = fmt.Sprintf(
			"Cancelling %s before the start refunds %d%%, less the non-refundable charge, under %q.",
			describeHours(tierHours), pct, p.Name)
	default:
		q.Explanation = fmt.Sprintf(
			"Cancelling %s or more before the start refunds %d%% under %q.",
			describeHours(tierHours), pct, p.Name)
	}
	return q
}

// QuoteFor resolves the policy and quotes in one call.
func (s *Service) QuoteFor(ctx context.Context, b domain.Booking, paidCents int, now time.Time) (domain.Quote, error) {
	p, err := s.For(ctx, b.FacilityID)
	if err != nil {
		return domain.Quote{}, err
	}
	return Quote(p, b, paidCents, now), nil
}

// ModificationCutoff returns the policy's reschedule cutoff for a facility.
// A booking closer to its start than this may not be moved.
func (s *Service) ModificationCutoff(ctx context.Context, facilityID string) (time.Duration, error) {
	p, err := s.For(ctx, facilityID)
	if err != nil {
		return 0, err
	}
	return time.Duration(p.ModificationCutoffHours) * time.Hour, nil
}

// tierFor picks the most generous tier the cancellation qualifies for. Tiers are
// sorted descending, so the first match is the largest HoursBefore satisfied.
func tierFor(p domain.CancellationPolicy, hoursUntilStart float64) (percent, tierHours int, ok bool) {
	tiers := append([]domain.RefundTier(nil), p.Tiers...)
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].HoursBefore > tiers[j].HoursBefore })
	for _, t := range tiers {
		if hoursUntilStart >= float64(t.HoursBefore) {
			return t.RefundPercent, t.HoursBefore, true
		}
	}
	return 0, 0, false
}

// smallestTierHours is the tightest window that still refunds anything, used to
// explain a non-refundable cancellation in the resident's terms.
func smallestTierHours(p domain.CancellationPolicy) int {
	smallest := 0
	for _, t := range p.Tiers {
		if t.RefundPercent > 0 && (smallest == 0 || t.HoursBefore < smallest) {
			smallest = t.HoursBefore
		}
	}
	return smallest
}

// describeHours renders an hour count the way a person would say it.
func describeHours(h int) string {
	switch {
	case h == 0:
		return "the start"
	case h%24 == 0 && h >= 24:
		days := h / 24
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	case h == 1:
		return "1 hour"
	default:
		return fmt.Sprintf("%d hours", h)
	}
}
