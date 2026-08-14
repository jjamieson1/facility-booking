package payment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	return NewService(db, Fixed(NewMockProvider())), db
}

func seedBooking(t *testing.T, db *gorm.DB, fee int) domain.Booking {
	t.Helper()
	f := domain.Facility{Name: "Hall", FeeCents: fee}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	sub := uuid.NewString()
	u := domain.User{Subject: sub, Name: "Ada Payer", Email: sub + "@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	b := domain.Booking{FacilityID: f.ID, UserID: u.ID, Status: domain.StatusConfirmed, FeeCents: fee}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func txnsFor(t *testing.T, db *gorm.DB, bookingID string) []domain.PaymentTransaction {
	t.Helper()
	var out []domain.PaymentTransaction
	if err := db.Where("booking_id = ?", bookingID).Order("created_at").Find(&out).Error; err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPaySuccessRecordsSucceededTxn(t *testing.T) {
	svc, db := newService(t)
	b := seedBooking(t, db, 5000)

	pay, err := svc.Pay(context.Background(), b.ID, SuccessCard)
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if pay.Status != domain.PayPaid {
		t.Fatalf("status = %q, want paid", pay.Status)
	}

	txns := txnsFor(t, db, b.ID)
	if len(txns) != 1 {
		t.Fatalf("got %d txns, want 1", len(txns))
	}
	tx := txns[0]
	if tx.Kind != domain.TxnCharge || tx.Status != domain.TxnSucceeded {
		t.Fatalf("txn kind/status = %s/%s, want charge/succeeded", tx.Kind, tx.Status)
	}
	if tx.PaymentID != pay.ID {
		t.Errorf("txn.PaymentID = %q, want %q", tx.PaymentID, pay.ID)
	}
	if tx.AmountCents != 5000 || tx.CardLast4 != "4242" || tx.Message == "" {
		t.Errorf("txn = %+v, want amount 5000 / last4 4242 / non-empty message", tx)
	}
}

func TestPayDeclineRecordsFailedTxnAndNoPayment(t *testing.T) {
	svc, db := newService(t)
	b := seedBooking(t, db, 5000)

	if _, err := svc.Pay(context.Background(), b.ID, DeclineCard); err != ErrDeclined {
		t.Fatalf("Pay err = %v, want ErrDeclined", err)
	}

	// No Payment row should exist for a decline.
	var payCount int64
	db.Model(&domain.Payment{}).Where("booking_id = ?", b.ID).Count(&payCount)
	if payCount != 0 {
		t.Fatalf("payment rows = %d, want 0", payCount)
	}

	txns := txnsFor(t, db, b.ID)
	if len(txns) != 1 {
		t.Fatalf("got %d txns, want 1", len(txns))
	}
	tx := txns[0]
	if tx.Kind != domain.TxnCharge || tx.Status != domain.TxnFailed {
		t.Fatalf("txn kind/status = %s/%s, want charge/failed", tx.Kind, tx.Status)
	}
	if tx.PaymentID != "" {
		t.Errorf("failed txn should have no PaymentID, got %q", tx.PaymentID)
	}
	if tx.Message == "" {
		t.Error("failed txn should carry a provider message")
	}
}

func TestRefundRecordsRefundTxn(t *testing.T) {
	svc, db := newService(t)
	b := seedBooking(t, db, 5000)
	if _, err := svc.Pay(context.Background(), b.ID, SuccessCard); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refund(context.Background(), b.ID); err != nil {
		t.Fatalf("Refund: %v", err)
	}

	txns := txnsFor(t, db, b.ID)
	if len(txns) != 2 {
		t.Fatalf("got %d txns, want 2 (charge + refund)", len(txns))
	}
	if txns[1].Kind != domain.TxnRefund || txns[1].Status != domain.TxnSucceeded {
		t.Fatalf("last txn = %s/%s, want refund/succeeded", txns[1].Kind, txns[1].Status)
	}
}

func TestReconcileWindowScopesTotalsAndRows(t *testing.T) {
	svc, db := newService(t)
	b := seedBooking(t, db, 1000)

	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 12, 0, 0, 0, time.Local) }
	// Three succeeded charges on three different days.
	for _, at := range []time.Time{day(2026, 3, 1), day(2026, 3, 15), day(2026, 4, 20)} {
		txn := domain.PaymentTransaction{
			Base:      domain.Base{ID: uuid.NewString(), CreatedAt: at},
			BookingID: b.ID, PaymentID: uuid.NewString(), Kind: domain.TxnCharge, Status: domain.TxnSucceeded,
			AmountCents: 1000, Provider: "mock", CardLast4: "4242",
		}
		if err := db.Create(&txn).Error; err != nil {
			t.Fatal(err)
		}
	}

	// A March window should include exactly the two March charges.
	rec, err := svc.Reconcile(context.Background(), Params{Window: Window{From: day(2026, 3, 1), To: day(2026, 3, 31)}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rec.Summary.Succeeded != 2 || rec.Summary.CollectedCents != 2000 {
		t.Errorf("March summary = %d charges / %d cents, want 2 / 2000", rec.Summary.Succeeded, rec.Summary.CollectedCents)
	}
	if rec.Total != 2 || len(rec.Transactions) != 2 {
		t.Fatalf("March total/rows = %d/%d, want 2/2", rec.Total, len(rec.Transactions))
	}

	// A single-day window (inclusive) catches just that day.
	rec, err = svc.Reconcile(context.Background(), Params{Window: Window{From: day(2026, 3, 15), To: day(2026, 3, 15)}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Summary.Succeeded != 1 || rec.Total != 1 || len(rec.Transactions) != 1 {
		t.Errorf("single-day = %d charges / %d total / %d rows, want 1/1/1", rec.Summary.Succeeded, rec.Total, len(rec.Transactions))
	}
}

func TestReconcilePagingAndFilter(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()
	b := seedBooking(t, db, 1000)

	// 5 succeeded charges + 3 failures, each on its own minute so order is stable.
	mk := func(i int, status domain.TxnStatus) {
		txn := domain.PaymentTransaction{
			Base:        domain.Base{ID: uuid.NewString(), CreatedAt: time.Date(2026, 5, 1, 0, i, 0, 0, time.Local)},
			BookingID:   b.ID,
			Kind:        domain.TxnCharge,
			Status:      status,
			AmountCents: 1000,
			Provider:    "mock",
		}
		if err := db.Create(&txn).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		mk(i, domain.TxnSucceeded)
	}
	for i := 5; i < 8; i++ {
		mk(i, domain.TxnFailed)
	}

	// Page 0 of 3: total counts the whole set, page holds 3 rows.
	rec, err := svc.Reconcile(ctx, Params{Page: 0, PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Total != 8 || len(rec.Transactions) != 3 || rec.PageSize != 3 {
		t.Fatalf("page0 = total %d / rows %d / size %d, want 8/3/3", rec.Total, len(rec.Transactions), rec.PageSize)
	}
	// Page 2 is the tail: 8 rows, size 3 → rows 7,8 → 2 on the last page.
	rec, err = svc.Reconcile(ctx, Params{Page: 2, PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Transactions) != 2 {
		t.Fatalf("page2 rows = %d, want 2", len(rec.Transactions))
	}

	// Filter narrows Total and the page to failures only; Summary is unaffected.
	rec, err = svc.Reconcile(ctx, Params{Filter: "failed", PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Total != 3 || len(rec.Transactions) != 3 {
		t.Fatalf("failed filter = total %d / rows %d, want 3/3", rec.Total, len(rec.Transactions))
	}
	for _, tx := range rec.Transactions {
		if tx.Status != domain.TxnFailed {
			t.Errorf("filtered row has status %q, want failed", tx.Status)
		}
	}
	if rec.Summary.Succeeded != 5 {
		t.Errorf("summary should ignore the filter: succeeded = %d, want 5", rec.Summary.Succeeded)
	}
}

func TestReconcileSummary(t *testing.T) {
	svc, db := newService(t)
	ctx := context.Background()

	paid := seedBooking(t, db, 5000)
	if _, err := svc.Pay(ctx, paid.ID, SuccessCard); err != nil {
		t.Fatal(err)
	}
	refunded := seedBooking(t, db, 8000)
	if _, err := svc.Pay(ctx, refunded.ID, SuccessCard); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refund(ctx, refunded.ID); err != nil {
		t.Fatal(err)
	}
	declined := seedBooking(t, db, 3000)
	if _, err := svc.Pay(ctx, declined.ID, DeclineCard); err != ErrDeclined {
		t.Fatal(err)
	}

	rec, err := svc.Reconcile(ctx, Params{})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	s := rec.Summary
	if s.CollectedCents != 13000 { // 5000 + 8000
		t.Errorf("collected = %d, want 13000", s.CollectedCents)
	}
	if s.RefundedCents != 8000 {
		t.Errorf("refunded = %d, want 8000", s.RefundedCents)
	}
	if s.NetCents != 5000 {
		t.Errorf("net = %d, want 5000", s.NetCents)
	}
	if s.Succeeded != 2 || s.Failed != 1 || s.Refunds != 1 {
		t.Errorf("counts = %d/%d/%d, want succeeded 2, failed 1, refunds 1", s.Succeeded, s.Failed, s.Refunds)
	}

	// Newest first, and the join populated booking context.
	if len(rec.Transactions) != 4 {
		t.Fatalf("got %d rows, want 4", len(rec.Transactions))
	}
	if rec.Transactions[0].FacilityName != "Hall" || rec.Transactions[0].UserEmail == "" {
		t.Errorf("row missing joined context: %+v", rec.Transactions[0])
	}
}
