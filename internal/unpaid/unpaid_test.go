package unpaid

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// billed seeds a booking with a payment in the given state, raised at billedAt.
func billed(t *testing.T, db *gorm.DB, status domain.BookingStatus, pay domain.PaymentStatus, billedAt, starts time.Time) domain.Booking {
	t.Helper()
	f := domain.Facility{Name: "Hall", FeeCents: 15000}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	sub := uuid.NewString()
	u := domain.User{Subject: sub, Email: sub + "@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	b := domain.Booking{
		FacilityID: f.ID, UserID: u.ID, Status: status, FeeCents: 15000,
		StartsAt: starts, EndsAt: starts.Add(time.Hour),
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	p := domain.Payment{BookingID: b.ID, AmountCents: 15000, Status: pay, Provider: "c2"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	// created_at is set by GORM; move it back to simulate an older bill.
	if err := db.Model(&domain.Payment{}).Where("id = ?", p.ID).
		Update("created_at", billedAt).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func statusOf(t *testing.T, db *gorm.DB, id string) domain.BookingStatus {
	t.Helper()
	var b domain.Booking
	if err := db.First(&b, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return b.Status
}

func newSweeper(t *testing.T) (*Sweeper, *gorm.DB, *[]domain.Booking) {
	t.Helper()
	db := testdb.New(t)
	freed := &[]domain.Booking{}
	s := NewSweeper(db, notify.NewLogNotifier(), 24*time.Hour, time.Minute, func(b domain.Booking) {
		*freed = append(*freed, b)
	})
	return s, db, freed
}

// One unpaid request must not hold a popular slot until its own start date.
func TestReleasesBookingUnpaidPastTheHoldWindow(t *testing.T) {
	s, db, freed := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusPending, domain.PayPending, now.Add(-25*time.Hour), now.Add(72*time.Hour))

	if err := s.Scan(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", got)
	}
	// The freed slot is the point: the waitlist has to hear about it.
	if len(*freed) != 1 {
		t.Fatalf("waitlist not notified: %d callbacks", len(*freed))
	}
}

// Inside the window the resident is still entitled to pay.
func TestKeepsBookingStillInsideTheHoldWindow(t *testing.T) {
	s, db, _ := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusPending, domain.PayPending, now.Add(-23*time.Hour), now.Add(72*time.Hour))

	if err := s.Scan(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.StatusPending {
		t.Fatalf("status = %q, want pending", got)
	}
}

// A paid booking is not a candidate however old the bill is — the single worst
// failure this sweeper could have.
func TestNeverReleasesAPaidBooking(t *testing.T) {
	s, db, _ := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusConfirmed, domain.PayPaid, now.Add(-100*24*time.Hour), now.Add(72*time.Hour))

	if err := s.Scan(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.StatusConfirmed {
		t.Fatalf("released a paid booking: status = %q", got)
	}
}

// Releasing a slot someone may already be standing in helps nobody.
func TestLeavesBookingsThatHaveAlreadyStarted(t *testing.T) {
	s, db, _ := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusConfirmed, domain.PayPending, now.Add(-48*time.Hour), now.Add(-time.Hour))

	if err := s.Scan(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.StatusConfirmed {
		t.Fatalf("status = %q, want confirmed", got)
	}
}

// A freed slot must never leave a payment reading as outstanding.
func TestReleaseClearsThePendingBill(t *testing.T) {
	s, db, _ := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusPending, domain.PayPending, now.Add(-25*time.Hour), now.Add(72*time.Hour))

	if err := s.Scan(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var p domain.Payment
	if err := db.First(&p, "booking_id = ?", b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if p.Status != domain.PayUnpaid {
		t.Fatalf("payment status = %q, want unpaid", p.Status)
	}
}

// Already-cancelled bookings must not be swept again into duplicate audit rows.
func TestSkipsAlreadyCancelledBookings(t *testing.T) {
	s, db, freed := newSweeper(t)
	now := time.Now()
	b := billed(t, db, domain.StatusPending, domain.PayPending, now.Add(-25*time.Hour), now.Add(72*time.Hour))

	for i := 0; i < 3; i++ {
		if err := s.Scan(context.Background(), now); err != nil {
			t.Fatal(err)
		}
	}
	if len(*freed) != 1 {
		t.Fatalf("swept %d times, want 1", len(*freed))
	}
	var n int64
	db.Model(&domain.AuditLog{}).Where("target_id = ? AND action = ?", b.ID, "booking.released.unpaid").Count(&n)
	if n != 1 {
		t.Fatalf("%d audit rows, want 1", n)
	}
}
