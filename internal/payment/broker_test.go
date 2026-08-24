package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// fakeHosted stands in for a gateway that hosts its own checkout, so these tests
// exercise Service's branch rather than C2's wire format (covered in internal/c2).
type fakeHosted struct {
	bills    []Bill
	payURL   string
	ref      string
	raiseErr error
}

func (f *fakeHosted) Name() string { return "fake-hosted" }
func (f *fakeHosted) Charge(context.Context, int, string) (Charge, error) {
	return Charge{}, errors.New("Charge must never be reached on a hosted gateway")
}
func (f *fakeHosted) Refund(context.Context, string, int) (string, error) {
	return "", ErrRefundNotSupported
}
func (f *fakeHosted) RaiseBill(_ context.Context, b Bill) (Hosted, error) {
	f.bills = append(f.bills, b)
	if f.raiseErr != nil {
		return Hosted{}, f.raiseErr
	}
	return Hosted{Ref: f.ref, PayURL: f.payURL}, nil
}

func hostedService(t *testing.T, p *fakeHosted) (*Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	return NewService(db, Fixed(p)), db
}

// Billing is not being paid. A hosted gateway settles out of band, so the
// payment sits pending until the gateway says otherwise — marking it paid here
// would confirm bookings nobody has paid for and inflate the §4.8 revenue report.
func TestHostedPayRecordsPendingNotPaid(t *testing.T) {
	p := &fakeHosted{payURL: "https://portal/pay/9c2a", ref: "9c2a"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)

	pay, err := svc.Pay(context.Background(), b.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pay.Status != domain.PayPending {
		t.Fatalf("status = %q, want pending", pay.Status)
	}
	if pay.PayURL != "https://portal/pay/9c2a" || pay.ProviderRef != "9c2a" {
		t.Fatalf("got %+v", pay)
	}
	// The ledger must not claim a succeeded charge for money that never arrived.
	for _, txn := range txnsFor(t, db, b.ID) {
		if txn.Kind == domain.TxnCharge && txn.Status == domain.TxnSucceeded {
			t.Fatalf("ledger shows a succeeded charge before payment: %+v", txn)
		}
	}
}

// The bill reference is derived from the booking id, and that stability is what
// makes the gateway's idempotency work — a random reference would bill twice on
// a retry.
func TestHostedBillReferenceIsStableAcrossRetries(t *testing.T) {
	p := &fakeHosted{payURL: "https://portal/pay/1", ref: "1"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)

	if _, err := svc.Pay(context.Background(), b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Pay(context.Background(), b.ID, ""); err != nil {
		t.Fatal(err)
	}
	if len(p.bills) != 2 {
		t.Fatalf("expected two attempts, got %d", len(p.bills))
	}
	if p.bills[0].Ref != p.bills[1].Ref {
		t.Fatalf("reference changed between attempts: %q vs %q", p.bills[0].Ref, p.bills[1].Ref)
	}
	if p.bills[0].Ref != BillRef(b.ID) {
		t.Fatalf("ref %q is not derived from the booking id", p.bills[0].Ref)
	}
}

// The gateway is told a subject, never a name or an email — it resolves the
// person itself, and sending identifiers it did not ask for leaks personal data.
func TestHostedBillCarriesSubjectNotPersonalData(t *testing.T) {
	p := &fakeHosted{payURL: "u", ref: "r"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)

	if _, err := svc.Pay(context.Background(), b.ID, ""); err != nil {
		t.Fatal(err)
	}
	var u domain.User
	if err := db.First(&u, "id = ?", b.UserID).Error; err != nil {
		t.Fatal(err)
	}
	bill := p.bills[0]
	if bill.Subject != u.Subject {
		t.Fatalf("subject = %q, want %q", bill.Subject, u.Subject)
	}
	if bill.Description == "" {
		t.Fatal("the citizen sees this on their invoice; it must not be blank")
	}
}

// A guest booked without an account (FAC-24), so no external gateway knows them.
// Rejected here rather than sent to the gateway to fail.
func TestHostedPayRejectsGuestBooker(t *testing.T) {
	p := &fakeHosted{payURL: "u", ref: "r"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)
	if err := db.Model(&domain.User{}).Where("id = ?", b.UserID).
		Update("subject", "guest:"+uuid.NewString()).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Pay(context.Background(), b.ID, ""); !errors.Is(err, ErrNoPayerIdentity) {
		t.Fatalf("got %v, want ErrNoPayerIdentity", err)
	}
	if len(p.bills) != 0 {
		t.Fatal("a guest must not reach the gateway at all")
	}
}

// A gateway that refuses refund instructions must leave a debt behind, not a
// silent no-op — the resident is owed money either way.
func TestRefundRecordsObligationWhenGatewayRefuses(t *testing.T) {
	p := &fakeHosted{payURL: "u", ref: "inv-1"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)
	if err := db.Create(&domain.Payment{
		BookingID: b.ID, AmountCents: 15000, Status: domain.PayPaid,
		Provider: p.Name(), ProviderRef: "inv-1",
	}).Error; err != nil {
		t.Fatal(err)
	}

	pay, err := svc.RefundAmount(context.Background(), b.ID, 7500, "50% under Municipal default")
	if !errors.Is(err, ErrRefundNotSupported) {
		t.Fatalf("got %v, want ErrRefundNotSupported", err)
	}
	// The money is genuinely still held, so the payment stays paid — reporting it
	// refunded would understate revenue for money nobody returned.
	if pay.Status != domain.PayPaid {
		t.Fatalf("status = %q, want paid", pay.Status)
	}

	obs, err := svc.Obligations(context.Background(), domain.RefundOwed)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 {
		t.Fatalf("expected one obligation, got %d", len(obs))
	}
	if obs[0].AmountCents != 7500 || obs[0].ProviderRef != "inv-1" {
		t.Fatalf("got %+v", obs[0])
	}
	if obs[0].Reason == "" {
		t.Fatal("an operator needs to know why this is owed")
	}
}

// A double-clicked cancel must not owe the resident twice.
func TestRefundObligationIsIdempotent(t *testing.T) {
	p := &fakeHosted{payURL: "u", ref: "inv-1"}
	svc, db := hostedService(t, p)
	b := seedBooking(t, db, 15000)
	if err := db.Create(&domain.Payment{
		BookingID: b.ID, AmountCents: 15000, Status: domain.PayPaid,
		Provider: p.Name(), ProviderRef: "inv-1",
	}).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.RefundAmount(context.Background(), b.ID, 7500, "policy"); !errors.Is(err, ErrRefundNotSupported) {
			t.Fatal(err)
		}
	}
	obs, _ := svc.Obligations(context.Background(), domain.RefundOwed)
	if len(obs) != 1 {
		t.Fatalf("expected one obligation, got %d", len(obs))
	}
}
