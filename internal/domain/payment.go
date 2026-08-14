package domain

// PaymentStatus tracks a booking's payment lifecycle. The demo uses a simulated
// provider (mock Stripe); the same states apply when a real provider is wired.
type PaymentStatus string

const (
	PayUnpaid   PaymentStatus = "unpaid"
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
