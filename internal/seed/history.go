package seed

import (
	"math/rand"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

var purposes = []string{
	"Community meeting", "Birthday party", "Yoga class", "Hockey practice", "Wedding reception",
	"AGM", "Soccer match", "Art workshop", "Fundraiser", "Dance rehearsal", "Book club",
	"Council session", "Fitness class", "Craft fair", "Rehearsal", "Team practice", "Reunion",
}

// seedHistory generates a year of booking history + payments so the reporting
// dashboard is populated. Bookings are weighted toward popular facilities and
// toward recent months (a rising trend), mostly confirmed with a realistic tail
// of cancellations/denials, plus a handful of upcoming and pending bookings.
func seedHistory(tx *gorm.DB, facilities []facilitySpec, bookers []*domain.User) error {
	rng := rand.New(rand.NewSource(20260724))
	now := nowFn()

	totalWeight := 0
	for _, f := range facilities {
		totalWeight += f.weight
	}
	pickFacility := func() facilitySpec {
		n := rng.Intn(totalWeight)
		for _, f := range facilities {
			if n < f.weight {
				return f
			}
			n -= f.weight
		}
		return facilities[len(facilities)-1]
	}

	var bookings []domain.Booking
	var payments []domain.Payment
	var txns []domain.PaymentTransaction

	add := func(b domain.Booking, paid, refunded bool) {
		b.ID = uuid.NewString()
		bookings = append(bookings, b)
		if b.FeeCents == 0 {
			return
		}
		if !(paid || refunded) {
			// Some abandoned bookings carry a decline the resident never retried.
			if b.Status == domain.StatusCancelled && rng.Float64() < 0.2 {
				at := b.CreatedAt
				if at.IsZero() {
					at = now.Add(-time.Duration(rng.Intn(240)) * time.Hour)
				}
				txns = append(txns, failedTxn(rng, b.ID, b.FeeCents, at))
			}
			return
		}
		payID := uuid.NewString()
		status := domain.PayPaid
		if refunded {
			status = domain.PayRefunded
		}
		payments = append(payments, domain.Payment{
			Base:      domain.Base{ID: payID},
			BookingID: b.ID, AmountCents: b.FeeCents, Status: status,
			Provider: "mock", ProviderRef: "seed_" + b.ID[:8],
		})

		// Ledger: the settled charge, plus a refund entry when reversed. ~8% of
		// charges were preceded by a decline (a retry that then succeeded).
		chargedAt := b.CreatedAt
		if chargedAt.IsZero() {
			chargedAt = now.Add(-time.Duration(rng.Intn(240)) * time.Hour)
		}
		if rng.Float64() < 0.08 {
			txns = append(txns, failedTxn(rng, b.ID, b.FeeCents, chargedAt.Add(-4*time.Minute)))
		}
		txns = append(txns, chargeTxn(b.ID, payID, b.FeeCents, chargedAt))
		if refunded {
			txns = append(txns, refundTxn(b.ID, payID, b.FeeCents, chargedAt.Add(time.Duration(1+rng.Intn(6))*24*time.Hour)))
		}
	}

	// --- past bookings (the bulk of the history) ---------------------------
	for i := 0; i < historyBookings; i++ {
		fs := pickFacility()
		booker := bookers[rng.Intn(len(bookers))]
		start := pastStart(rng, now, fs.f)
		end := start.Add(time.Duration(durationHours(rng, fs.f)) * time.Hour)

		status := domain.StatusConfirmed
		paid, refunded := true, false
		switch r := rng.Float64(); {
		case r < 0.10: // cancelled — refunded if it had been paid
			status, refunded, paid = domain.StatusCancelled, rng.Float64() < 0.7, false
		case r < 0.16: // denied — never charged
			status, paid = domain.StatusDenied, false
		default: // confirmed; a few paid bookings get refunded later
			refunded = fs.f.FeeCents > 0 && rng.Float64() < 0.06
			paid = !refunded
		}

		add(domain.Booking{
			FacilityID: fs.f.ID, UserID: booker.ID, StartsAt: start, EndsAt: end, Status: status,
			Purpose: purposes[rng.Intn(len(purposes))], Attendance: 4 + rng.Intn(fs.f.Capacity/2+1),
			FeeCents: fs.f.FeeFor(booker.IsResident), Resident: booker.IsResident,
			Base: domain.Base{CreatedAt: start.Add(-time.Duration(3+rng.Intn(20)) * 24 * time.Hour)},
		}, paid, refunded)
	}

	// --- upcoming confirmed bookings ---------------------------------------
	for i := 0; i < 18; i++ {
		fs := pickFacility()
		booker := bookers[rng.Intn(len(bookers))]
		start := futureStart(rng, now, fs.f, 1, 40)
		end := start.Add(time.Duration(durationHours(rng, fs.f)) * time.Hour)
		paid := fs.f.FeeCents > 0 && rng.Float64() < 0.8
		add(domain.Booking{
			FacilityID: fs.f.ID, UserID: booker.ID, StartsAt: start, EndsAt: end, Status: domain.StatusConfirmed,
			Purpose: purposes[rng.Intn(len(purposes))], Attendance: 4 + rng.Intn(fs.f.Capacity/2+1),
			FeeCents: fs.f.FeeFor(booker.IsResident), Resident: booker.IsResident,
		}, paid, false)
	}

	// --- pending approvals (some older than 24h) ---------------------------
	approvalFacilities := filterApproval(facilities)
	for i := 0; i < 6; i++ {
		fs := approvalFacilities[rng.Intn(len(approvalFacilities))]
		booker := bookers[rng.Intn(len(bookers))]
		start := futureStart(rng, now, fs.f, 3, 45)
		end := start.Add(time.Duration(durationHours(rng, fs.f)) * time.Hour)
		createdAgo := time.Duration(rng.Intn(6)*12) * time.Hour // 0–60h ago → some >24h
		add(domain.Booking{
			FacilityID: fs.f.ID, UserID: booker.ID, StartsAt: start, EndsAt: end, Status: domain.StatusPending,
			Purpose: purposes[rng.Intn(len(purposes))], Attendance: 4 + rng.Intn(fs.f.Capacity/2+1),
			FeeCents: fs.f.FeeFor(booker.IsResident), Resident: booker.IsResident,
			Base: domain.Base{CreatedAt: now.Add(-createdAgo)},
		}, false, false)
	}

	if err := tx.CreateInBatches(bookings, 200).Error; err != nil {
		return err
	}
	if len(payments) > 0 {
		if err := tx.CreateInBatches(payments, 200).Error; err != nil {
			return err
		}
	}
	if len(txns) > 0 {
		if err := tx.CreateInBatches(txns, 200).Error; err != nil {
			return err
		}
	}
	return nil
}

// declineMessages are the provider reasons shown for a failed charge.
var declineMessages = []string{
	"Card declined — insufficient funds.",
	"Card declined — do not honor.",
	"Card declined — expired card.",
	"Card declined — incorrect CVC.",
	"Processing error — issuer unavailable, please retry.",
}

var declineCards = []string{"0002", "9995", "0069", "0127", "0119"}

func chargeTxn(bookingID, payID string, cents int, at time.Time) domain.PaymentTransaction {
	return domain.PaymentTransaction{
		Base:      domain.Base{ID: uuid.NewString(), CreatedAt: at},
		BookingID: bookingID, PaymentID: payID, Kind: domain.TxnCharge, Status: domain.TxnSucceeded,
		AmountCents: cents, Provider: "mock", ProviderRef: "ch_" + uuid.NewString()[:16],
		CardLast4: "4242", Message: "Approved.",
	}
}

func refundTxn(bookingID, payID string, cents int, at time.Time) domain.PaymentTransaction {
	return domain.PaymentTransaction{
		Base:      domain.Base{ID: uuid.NewString(), CreatedAt: at},
		BookingID: bookingID, PaymentID: payID, Kind: domain.TxnRefund, Status: domain.TxnSucceeded,
		AmountCents: cents, Provider: "mock", ProviderRef: "re_" + uuid.NewString()[:16],
		CardLast4: "4242", Message: "Refund issued to the original card.",
	}
}

func failedTxn(rng *rand.Rand, bookingID string, cents int, at time.Time) domain.PaymentTransaction {
	return domain.PaymentTransaction{
		Base:      domain.Base{ID: uuid.NewString(), CreatedAt: at},
		BookingID: bookingID, Kind: domain.TxnCharge, Status: domain.TxnFailed,
		AmountCents: cents, Provider: "mock", ProviderRef: "",
		CardLast4: declineCards[rng.Intn(len(declineCards))], Message: declineMessages[rng.Intn(len(declineMessages))],
	}
}

// pastStart picks a start time in the past year, biased toward recent months so
// the utilization trend rises, on a random open-hours slot for the facility.
func pastStart(rng *rand.Rand, now time.Time, f domain.Facility) time.Time {
	// Bias: square the uniform so more picks land near "now".
	frac := rng.Float64() * rng.Float64() // skew toward 0 (recent)
	daysAgo := 1 + int(frac*360)
	day := now.AddDate(0, 0, -daysAgo)
	return atOpenSlot(rng, day, f)
}

func futureStart(rng *rand.Rand, now time.Time, f domain.Facility, minDays, maxDays int) time.Time {
	day := now.AddDate(0, 0, minDays+rng.Intn(maxDays-minDays+1))
	return atOpenSlot(rng, day, f)
}

// atOpenSlot returns an on-the-hour start time within the facility's 08:00–22:00
// window that leaves room for a booking before close.
func atOpenSlot(rng *rand.Rand, day time.Time, f domain.Facility) time.Time {
	maxHours := f.MaxMinutes / 60
	if maxHours < 1 {
		maxHours = 2
	}
	latestStart := 22 - maxHours
	if latestStart < 8 {
		latestStart = 8
	}
	hour := 8 + rng.Intn(latestStart-8+1)
	return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, day.Location())
}

func durationHours(rng *rand.Rand, f domain.Facility) int {
	minH, maxH := f.MinMinutes/60, f.MaxMinutes/60
	if minH < 1 {
		minH = 1
	}
	if maxH < minH {
		maxH = minH
	}
	// Cap at 4h so a year of bookings spreads across days rather than filling one.
	if maxH > 4 {
		maxH = 4
	}
	return minH + rng.Intn(maxH-minH+1)
}

func filterApproval(facilities []facilitySpec) []facilitySpec {
	var out []facilitySpec
	for _, f := range facilities {
		if f.f.RequiresApproval {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return facilities
	}
	return out
}
