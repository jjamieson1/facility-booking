package domain

// PaymentStatus tracks a booking's payment lifecycle. The demo uses a simulated
// provider (mock Stripe); the same states apply when a real provider is wired.
type PaymentStatus string

const (
	PayUnpaid PaymentStatus = "unpaid"
	// PayPending means a bill has been raised and the payer has been sent to the
	// gateway's own checkout, but no money has arrived. Hosted gateways like C2's
	// payment broker settle out of band, so "asked for" and "paid" are different
	// states — collapsing them would mark a booking paid the moment it was billed.
	PayPending  PaymentStatus = "pending"
	PayPaid     PaymentStatus = "paid"
	PayRefunded PaymentStatus = "refunded"
)

// Payment records the money side of a booking. Provider/ProviderRef identify
// which gateway handled it ("mock" in the demo) and its reference id, so a real
// Stripe integration slots in without a schema change.
type Payment struct {
	Base
	BookingID   string        `gorm:"type:varchar(36);uniqueIndex" json:"bookingId"`
	AmountCents int           `json:"amountCents"`
	Status      PaymentStatus `gorm:"type:varchar(20);default:unpaid" json:"status"`
	Provider    string        `gorm:"type:varchar(40)" json:"provider"`
	ProviderRef string        `gorm:"type:varchar(120)" json:"providerRef"`
	// PayURL is the gateway-hosted checkout the payer is sent to, set only by
	// hosted providers. Stored rather than rebuilt because it is the gateway's
	// URL, not ours, and the payer may come back to it days later.
	PayURL string `gorm:"type:varchar(500)" json:"payUrl,omitempty"`
}

// RefundObligationStatus is whether money still needs returning.
type RefundObligationStatus string

const (
	RefundOwed    RefundObligationStatus = "owed"
	RefundSettled RefundObligationStatus = "settled"
)

// RefundObligation records money this app owes a resident but cannot return
// itself.
//
// It exists because a hosted gateway need not let the billing application
// initiate a refund — C2's partner API has no refund endpoint at all; refunds
// are an operator action inside C2, gated by WRITE_PAYMENTS. The cancellation
// still has to happen (the slot must free immediately), and the resident is
// still owed the policy amount, so the debt is recorded here and settled when
// the operator actions it. Without this the refund would exist only in an audit
// line nobody queries.
type RefundObligation struct {
	Base
	BookingID   string                 `gorm:"type:varchar(36);index" json:"bookingId"`
	PaymentID   string                 `gorm:"type:varchar(36);index" json:"paymentId"`
	AmountCents int                    `json:"amountCents"`
	Currency    string                 `gorm:"type:varchar(3);default:CAD" json:"currency"`
	Reason      string                 `gorm:"type:varchar(300)" json:"reason"`
	Status      RefundObligationStatus `gorm:"type:varchar(20);index;default:owed" json:"status"`
	// Provider and ProviderRef say where the operator must go to action it —
	// "refund invoice INV-… in C2" is the whole point of the record.
	Provider    string `gorm:"type:varchar(40)" json:"provider"`
	ProviderRef string `gorm:"type:varchar(120)" json:"providerRef"`
	// SettledRef is the gateway's refund id, learned from the settlement
	// callback; SettledCents is what was actually returned, which need not equal
	// what was owed if the operator overrode it.
	SettledRef   string   `gorm:"type:varchar(120)" json:"settledRef,omitempty"`
	SettledCents int      `json:"settledCents,omitempty"`
	Booking      *Booking `gorm:"foreignKey:BookingID" json:"booking,omitempty"`
}

// TxnKind distinguishes a money-in charge from a money-out refund.
type TxnKind string

const (
	TxnCharge TxnKind = "charge"
	TxnRefund TxnKind = "refund"
)

// TxnStatus is the outcome the gateway reported for a single attempt.
type TxnStatus string

const (
	TxnSucceeded TxnStatus = "succeeded"
	TxnFailed    TxnStatus = "failed"
	// TxnPending is a bill raised at a hosted gateway with no money yet. It has
	// to be distinct from succeeded: the reconciliation ledger showing a
	// "succeeded" charge for money that never arrived is worse than no row.
	TxnPending TxnStatus = "pending"
)

// PaymentTransaction is an append-only ledger of every gateway interaction —
// successful charges, declines, and refunds — so staff can reconcile the money
// against the provider. Unlike Payment (one current row per booking), a booking
// accrues many transactions (a decline then a retry, a charge then a refund).
// A failed charge has no PaymentID; the Message carries the provider's reason.
type PaymentTransaction struct {
	Base
	BookingID   string    `gorm:"type:varchar(36);index" json:"bookingId"`
	PaymentID   string    `gorm:"type:varchar(36);index" json:"paymentId"`
	Kind        TxnKind   `gorm:"type:varchar(20);index" json:"kind"`
	Status      TxnStatus `gorm:"type:varchar(20);index" json:"status"`
	AmountCents int       `json:"amountCents"`
	Provider    string    `gorm:"type:varchar(40)" json:"provider"`
	ProviderRef string    `gorm:"type:varchar(120)" json:"providerRef"`
	CardLast4   string    `gorm:"type:varchar(4)" json:"cardLast4"`
	Message     string    `gorm:"type:varchar(255)" json:"message"`
}
