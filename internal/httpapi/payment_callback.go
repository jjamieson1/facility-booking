package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/c2"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
)

// paymentCallbackHandler receives C2's signed settlement notices.
type paymentCallbackHandler struct {
	auth     *auth.Service
	payments *payment.Service
	audit    auditlog.Recorder
	notifier notify.Notifier
	bookings *booking.Service
}

// settle applies a payment or refund that C2 reports.
//
// Unauthenticated by design, and that is exactly why the token is everything:
// C2 posts server-to-server with no session and no shared secret, so the RS256
// signature over C2's published JWKS — plus issuer, audience and expiry — is the
// only thing standing between a stranger's POST and a booking marked paid. The
// verification lives in auth.VerifyPaymentToken; nothing here may bypass it.
//
// Always answers 2xx once the token verifies, even if applying it is a no-op.
// C2 logs non-2xx and retries are best-effort, so a 500 for a duplicate delivery
// would generate noise about an event already handled.
func (h paymentCallbackHandler) settle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "malformed callback")
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("status_token"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing status_token")
		return
	}

	claims, err := h.auth.VerifyPaymentToken(r.Context(), raw)
	if errors.Is(err, auth.ErrNotConfigured) {
		// No application id configured: we cannot check the audience, so we
		// cannot trust the token. Refuse rather than apply it unverified.
		writeError(w, http.StatusServiceUnavailable, "payment callbacks are not configured")
		return
	}
	if err != nil {
		// Deliberately terse. The caller is unauthenticated, and naming which
		// check failed helps someone probing for a forgery more than it helps C2.
		writeError(w, http.StatusUnauthorized, "invalid status token")
		return
	}

	cents, err := c2.AmountToCents(claims.EventAmount.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable amount")
		return
	}

	st := payment.Settlement{
		Ref: claims.ClientRef,
		// Branch on the event, not the invoice status: a partial refund arrives
		// as a refund event while the invoice is still PAID_ONLINE, so reading
		// the status would apply it as a payment.
		Refund:        claims.Event == "refund",
		AmountCents:   cents,
		GatewayRef:    claims.GatewayTxnID,
		FullyRefunded: claims.Status == c2.InvoiceRefunded,
	}

	pay, applied, err := h.payments.ApplySettlement(r.Context(), st)
	if errors.Is(err, payment.ErrUnknownBill) {
		// A verified token naming a bill we never raised. Acknowledged so C2
		// stops redelivering, but recorded: it means our references and C2's
		// have diverged, which is worth someone knowing about.
		h.recordAudit(r, "payment.settlement.unknown", "",
			"C2 reported a settlement for an unknown bill reference "+claims.ClientRef)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not apply the settlement")
		return
	}

	event := "payment.settled"
	if st.Refund {
		event = "payment.refunded"
	}
	h.recordAudit(r, event, pay.BookingID, describeSettlement(st))

	// Receipt only on a payment we actually applied. C2 re-delivers callbacks —
	// its own docs call delivery best-effort — so without the `applied` check a
	// resident would get a fresh receipt every time it retried. A refund is a
	// different message and is not one.
	//
	// Best-effort by construction: the notifier swallows its own failures, and
	// the settlement is committed either way. The booking is paid whether or not
	// the message lands.
	if applied && !st.Refund && h.notifier != nil {
		h.notifier.PaymentReceipt(pay.BookingID, st.AmountCents, st.GatewayRef)
	}

	// Money landing is what turns a held slot into a booking (FAC-52). Routed
	// through ConfirmIfSatisfied rather than setting the status here, because
	// that function is the only gate: a booking may also be carrying staff
	// conditions, and this path must not confirm one whose terms are unmet.
	if applied && !st.Refund {
		h.confirmIfPaid(r, pay.BookingID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// confirmIfPaid promotes a paid hold to a confirmed booking and tells the
// booker, with the invite.
//
// Best-effort, deliberately: the money is recorded and the callback has to be
// acknowledged so C2 stops redelivering. A booking left holding its slot after a
// successful payment is visible and fixable; a redelivery loop is not.
func (h paymentCallbackHandler) confirmIfPaid(r *http.Request, bookingID string) {
	if h.bookings == nil {
		return
	}
	// hasDocument=false is safe here: this path only completes a booking whose
	// sole outstanding item was the money. One still owing a document stays
	// unconfirmed, and the upload route runs the same gate again.
	b, confirmed, err := h.bookings.ConfirmIfSatisfied(r.Context(), bookingID, false)
	if err != nil || !confirmed || b == nil {
		return
	}
	if h.notifier != nil {
		full, _ := h.bookings.Get(r.Context(), bookingID)
		if full != nil {
			h.notifier.BookingConfirmed(*full, calendar.Invite(*full))
		}
	}
	h.recordAudit(r, "booking.paid.confirmed", bookingID, "Payment settled; booking confirmed")
}

// recordAudit mirrors the settlement to the central audit service. There is no
// actor: C2 acted, not a person, so the actor fields stay empty rather than
// being filled with a placeholder that would read as a staff member doing it.
func (h paymentCallbackHandler) recordAudit(_ *http.Request, action, bookingID, message string) {
	if h.audit == nil {
		return
	}
	h.audit.Record(auditlog.Event{
		Action: action, TargetType: "booking", TargetID: bookingID, Message: message,
	})
}

// describeSettlement is the audit line a person reads months later.
func describeSettlement(st payment.Settlement) string {
	kind := "Payment"
	if st.Refund {
		kind = "Refund"
	}
	return kind + " of " + c2.CentsToAmount(st.AmountCents) + " confirmed by C2 (" + st.GatewayRef + ")"
}
