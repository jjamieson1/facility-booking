package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Test cards for the demo — the same numbers Stripe uses in test mode, so the
// flow feels familiar without any real gateway.
const (
	SuccessCard = "4242424242424242"
	DeclineCard = "4000000000000002"
)

// MockProvider simulates a Stripe-style gateway. It never contacts a network and
// charges nothing; the decline card lets the demo show the failure path.
type MockProvider struct{}

// NewMockProvider returns the demo payment provider.
func NewMockProvider() *MockProvider { return &MockProvider{} }

func (MockProvider) Name() string { return "mock" }

// Charge approves any card except the well-known decline number. Either way it
// returns a Charge carrying the provider message + masked card so the outcome is
// recorded on the ledger.
func (MockProvider) Charge(_ context.Context, _ int, card string) (Charge, error) {
	last4 := cardLast4(card)
	if normalizeCard(card) == DeclineCard {
		return Charge{Status: domain.PayUnpaid, Message: "Card declined — do not honor (test card 4000 0000 0000 0002).", Last4: last4}, ErrDeclined
	}
	return Charge{Ref: "mock_" + randRef(), Status: domain.PayPaid, Message: "Approved.", Last4: last4}, nil
}

// Refund always succeeds in the mock, and reports the amount so a partial
// refund is visible on the demo's reconciliation screen.
func (MockProvider) Refund(_ context.Context, _ string, amountCents int) (string, error) {
	return fmt.Sprintf("Refund of %d.%02d issued to the original card.", amountCents/100, amountCents%100), nil
}

// cardLast4 returns the last four digits of the (demo) PAN, or "" if too short.
func cardLast4(card string) string {
	c := normalizeCard(card)
	if len(c) < 4 {
		return ""
	}
	return c[len(c)-4:]
}

func randRef() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
