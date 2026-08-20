package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/policy"
)

type policyHandler struct {
	policies *policy.Service
	bookings *booking.Service
}

// facilityPolicy publishes a facility's cancellation terms. Public and
// unauthenticated on purpose: §4.7 requires the resident to see the terms
// *before* committing to a booking, and the facility page is where that
// decision is made.
func (h policyHandler) facilityPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := h.policies.For(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the cancellation policy")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// refundQuote answers "what do I get back if I cancel now" for one booking, so
// the confirmation step can state the consequence rather than surprising the
// resident afterwards. The same computation runs in the cancel path, so the
// figure shown is the figure issued.
func (h policyHandler) refundQuote(w http.ResponseWriter, r *http.Request) {
	b, err := h.bookings.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	user := auth.FromContext(r.Context())
	if b.UserID != user.ID && !auth.IsStaff(user) {
		writeError(w, http.StatusForbidden, "not your booking")
		return
	}
	q, err := h.policies.QuoteFor(r.Context(), *b, paidCents(b), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not quote the refund")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// paidCents is what was actually paid, which is not the booking's fee: an unpaid
// booking refunds nothing however generous the policy, and a free facility is
// the same case at zero.
func paidCents(b *domain.Booking) int {
	if b.Payment == nil || b.Payment.Status != domain.PayPaid {
		return 0
	}
	return b.Payment.AmountCents
}
