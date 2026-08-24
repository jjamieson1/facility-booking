package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
	"github.com/jjamieson1/facility-booking/internal/facility"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
	"github.com/jjamieson1/facility-booking/internal/policy"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
	"github.com/jjamieson1/facility-booking/internal/waiver"
)

type bookingHandler struct {
	bookings     *booking.Service
	facilities   *facility.Service
	payments     *payment.Service
	waitlist     *waitlist.Service
	waiver       *waiver.Service
	entitlements *entitlement.Service
	policies     *policy.Service
	notifier     notify.Notifier
	audit        auditlog.Recorder
}

// priceFor resolves the booker's entitlements before any booking transaction
// opens (§P2-5.11a constraint 1) and returns them as the pricing applied to
// every occurrence of this request (constraint 2). One resolution per request,
// not one per occurrence.
func (h bookingHandler) priceFor(r *http.Request, user *domain.User) booking.Pricing {
	if h.entitlements == nil {
		return booking.Pricing{Resident: user.IsResident}
	}
	set := h.entitlements.Resolve(r.Context(), *user)
	return booking.Pricing{Resident: set.IsResident(), Stamp: set.Stamp("")}
}

// recordAudit mirrors a staff action to the central audit-logging service. It's
// best-effort (the Recorder never blocks) and complements the local audit row.
func (h bookingHandler) recordAudit(r *http.Request, action, bookingID, message string) {
	if h.audit == nil {
		return
	}
	user := auth.FromContext(r.Context())
	h.audit.Record(auditlog.Event{
		Action: action, ActorID: user.ID, ActorEmail: user.Email,
		TargetType: "booking", TargetID: bookingID, Message: message,
	})
}

type createBookingReq struct {
	FacilityID  string    `json:"facilityId"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Purpose     string    `json:"purpose"`
	Attendance  int       `json:"attendance"`
	RepeatWeeks int       `json:"repeatWeeks"` // >1 books a weekly-recurring slot (§4.11)
}

// create requests a booking for the current user. With repeatWeeks > 1 it books
// a weekly-recurring slot and returns the batch result.
func (h bookingHandler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req createBookingReq
	if !decode(w, r, &req) {
		return
	}

	pricing := h.priceFor(r, user)

	if req.RepeatWeeks > 1 {
		res, err := h.bookings.RequestRecurring(r.Context(), user.ID, req.FacilityID, req.Start, req.End, req.RepeatWeeks, req.Purpose, req.Attendance, pricing)
		if err != nil {
			bookingError(w, err)
			return
		}
		for i := range res.Created {
			h.notifyNew(r, res.Created[i])
		}
		writeJSON(w, http.StatusCreated, res)
		return
	}

	b, err := h.bookings.Request(r.Context(), user.ID, req.FacilityID, req.Start, req.End, req.Purpose, req.Attendance, pricing)
	if err != nil {
		bookingError(w, err)
		return
	}
	h.notifyNew(r, *b)
	writeJSON(w, http.StatusCreated, b)
}

// notifyNew fires the right notification for a freshly created booking.
func (h bookingHandler) notifyNew(r *http.Request, b domain.Booking) {
	if b.Status == domain.StatusPending {
		h.notifier.BookingSubmitted(b)
	} else {
		h.notifier.BookingConfirmed(b, h.inviteFor(r, b.ID))
	}
}

func (h bookingHandler) mine(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	list, err := h.bookings.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load bookings")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h bookingHandler) get(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, b)
}

// cancelResponse carries the booking plus what the cancellation refunded, so the
// SPA can tell the resident the outcome rather than leaving them to check a
// statement.
type cancelResponse struct {
	*domain.Booking
	Refund *domain.Quote `json:"refund,omitempty"`
}

func (h bookingHandler) cancel(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")

	// Quote BEFORE cancelling: the refund depends on how far ahead of the start
	// we are, and cancelling first would compute it against a booking that is
	// already gone. Reading it here also keeps the figure the resident was shown
	// and the figure issued the same computation.
	quote, quoted := h.quoteRefund(r, id)

	b, err := h.bookings.Cancel(r.Context(), user, id)
	if err != nil {
		bookingError(w, err)
		return
	}

	// Issue the refund AFTER the cancellation transaction has committed. A
	// gateway call inside it would hold the booking row locks for the provider's
	// latency — the same rule the entitlement resolution follows.
	var refund *domain.Quote
	if quoted && quote.RefundCents > 0 {
		_, err := h.payments.RefundAmount(r.Context(), b.ID, quote.RefundCents, quote.Explanation)
		switch {
		case errors.Is(err, payment.ErrRefundNotSupported):
			// The gateway does not take refund instructions from us (C2 refunds
			// are an operator action inside C2). The debt has been recorded as an
			// obligation, so this is a queued refund, not a failed one — auditing
			// it as a failure would send staff hunting for a fault that is not
			// there.
			h.recordAudit(r, "booking.refund.owed", b.ID,
				fmt.Sprintf("Refund of %d cents is owed and must be issued at the gateway: %s", quote.RefundCents, quote.Explanation))
		case err != nil:
			// The booking is cancelled and the slot is free; a failed refund must
			// not undo that or the resident is left holding a booking they asked
			// to drop. Surface it for staff instead.
			h.recordAudit(r, "booking.refund.failed", b.ID,
				fmt.Sprintf("Automatic refund of %d cents failed: %v", quote.RefundCents, err))
		default:
			h.recordAudit(r, "booking.refund", b.ID, quote.Explanation)
		}
	}
	if quoted {
		q := quote
		refund = &q
	}

	h.notifier.BookingCancelled(*b, h.inviteFor(r, b.ID))
	h.notifyWaitlist(r, *b) // a freed slot may open the waitlist (§4.11)
	if auth.IsStaff(user) && b.UserID != user.ID {
		h.recordAudit(r, "booking.cancel", b.ID, "Staff cancelled a booking")
	}
	writeJSON(w, http.StatusOK, cancelResponse{Booking: b, Refund: refund})
}

// quoteRefund works out what cancelling this booking now would return. A missing
// policy service or an unreadable booking simply means no automatic refund —
// never a failed cancellation.
func (h bookingHandler) quoteRefund(r *http.Request, bookingID string) (domain.Quote, bool) {
	if h.policies == nil {
		return domain.Quote{}, false
	}
	b, err := h.bookings.Get(r.Context(), bookingID)
	if err != nil {
		return domain.Quote{}, false
	}
	q, err := h.policies.QuoteFor(r.Context(), *b, paidCents(b), time.Now())
	if err != nil {
		return domain.Quote{}, false
	}
	return q, true
}

// notifyWaitlist tells anyone waitlisted for the freed booking's window.
func (h bookingHandler) notifyWaitlist(r *http.Request, b domain.Booking) {
	if h.waitlist != nil {
		_, _ = h.waitlist.NotifyFreed(r.Context(), b.FacilityID, b.StartsAt, b.EndsAt)
	}
}

type waitlistReq struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (h bookingHandler) joinWaitlist(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req waitlistReq
	if !decode(w, r, &req) {
		return
	}
	e, err := h.waitlist.Join(r.Context(), user.ID, chi.URLParam(r, "id"), req.Start, req.End)
	if errors.Is(err, waitlist.ErrFacilityNotFound) {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not join waitlist")
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h bookingHandler) myWaitlist(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	list, err := h.waitlist.ListForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load waitlist")
		return
	}
	if list == nil {
		list = []domain.WaitlistEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h bookingHandler) leaveWaitlist(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if err := h.waitlist.Leave(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not leave waitlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type rescheduleReq struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// reschedule moves a booking to a new time (owner or staff), re-sending the
// calendar invite on success.
func (h bookingHandler) reschedule(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req rescheduleReq
	if !decode(w, r, &req) {
		return
	}
	b, err := h.bookings.Reschedule(r.Context(), user, chi.URLParam(r, "id"), req.Start, req.End)
	if err != nil {
		bookingError(w, err)
		return
	}
	full, _ := h.bookings.Get(r.Context(), b.ID)
	if full.Status == domain.StatusConfirmed {
		h.notifier.BookingConfirmed(*full, h.inviteFor(r, b.ID))
	}
	if auth.IsStaff(user) && b.UserID != user.ID {
		h.recordAudit(r, "booking.reschedule", b.ID, "Staff rescheduled a booking")
	}
	writeJSON(w, http.StatusOK, b)
}

type payReq struct {
	Card string `json:"card"`
}

// pay charges the mock provider for a booking the caller owns.
func (h bookingHandler) pay(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	b, err := h.bookings.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if b.UserID != user.ID {
		writeError(w, http.StatusForbidden, "not your booking")
		return
	}
	var req payReq
	if !decode(w, r, &req) {
		return
	}
	pay, err := h.payments.Pay(r.Context(), b.ID, req.Card)
	if errors.Is(err, payment.ErrDeclined) {
		writeError(w, http.StatusPaymentRequired, "card declined")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "payment failed")
		return
	}
	writeJSON(w, http.StatusOK, pay)
}

// --- staff -----------------------------------------------------------------

func (h bookingHandler) pending(w http.ResponseWriter, r *http.Request) {
	list, err := h.bookings.ListPending(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load queue")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h bookingHandler) approve(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")

	// §4.11: a facility that requires a waiver cannot be confirmed until one is
	// on file.
	if existing, err := h.bookings.Get(r.Context(), id); err == nil &&
		existing.Facility != nil && existing.Facility.RequiresWaiver &&
		h.waiver != nil && !h.waiver.Has(r.Context(), id) {
		writeError(w, http.StatusConflict, "a signed waiver is required before this booking can be approved")
		return
	}

	b, err := h.bookings.Approve(r.Context(), user.ID, id)
	if err != nil {
		bookingError(w, err)
		return
	}
	full, _ := h.bookings.Get(r.Context(), b.ID)
	h.notifier.BookingConfirmed(*full, h.inviteFor(r, b.ID))
	h.recordAudit(r, "booking.approve", b.ID, "Staff approved a booking")
	writeJSON(w, http.StatusOK, b)
}

func (h bookingHandler) deny(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	b, err := h.bookings.Deny(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		bookingError(w, err)
		return
	}
	h.notifier.BookingDenied(*b)
	h.notifyWaitlist(r, *b)
	h.recordAudit(r, "booking.deny", b.ID, "Staff denied a booking")
	writeJSON(w, http.StatusOK, b)
}

// refundReq lets staff override the policy with a partial amount. An absent or
// zero amount means the full payment, which is what this endpoint always did.
type refundReq struct {
	AmountCents int    `json:"amountCents"`
	Reason      string `json:"reason"`
}

// refund is the staff override the cancellation policy explicitly allows for
// (§4.7): the automatic figure follows the policy, and staff can depart from it
// when circumstances warrant — audited either way, with the reason recorded so
// a departure from policy is explainable later.
func (h bookingHandler) refund(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")

	// The body is optional: an empty request is still a full refund.
	var req refundReq
	if r.ContentLength > 0 && !decode(w, r, &req) {
		return
	}

	pay, err := h.payments.RefundAmount(r.Context(), id, req.AmountCents, req.Reason)
	if errors.Is(err, payment.ErrRefundNotSupported) {
		// Recorded as owed rather than refunded. 202 rather than 200: staff have
		// been heard, but the money has not moved and they must finish the job at
		// the gateway. Reporting 200 would tell them it was done.
		h.recordAudit(r, "booking.refund.owed", id,
			fmt.Sprintf("Refund of %d cents recorded as owed; issue it at the gateway", req.AmountCents))
		writeJSON(w, http.StatusAccepted, pay)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "refund failed")
		return
	}

	detail := "Staff refunded a booking in full"
	if req.AmountCents > 0 {
		detail = fmt.Sprintf("Staff refunded %d cents (policy override)", req.AmountCents)
	}
	if req.Reason != "" {
		detail += ": " + req.Reason
	}
	// Refunds weren't audited before; record both locally and centrally.
	_ = h.bookings.Audit(r.Context(), user.ID, "booking.refund", id)
	h.recordAudit(r, "booking.refund", id, detail)
	writeJSON(w, http.StatusOK, pay)
}

// --- calendar --------------------------------------------------------------

// invite streams a single booking's .ics to its owner (or staff).
func (h bookingHandler) invite(w http.ResponseWriter, r *http.Request) {
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
	writeICS(w, "invite.ics", calendar.Invite(*b))
}

// feed streams the whole city's confirmed bookings as a subscribable calendar.
func (h bookingHandler) feed(w http.ResponseWriter, r *http.Request) {
	bookings, err := h.bookings.ListForCalendar(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build feed")
		return
	}
	writeICS(w, calendar.FeedFilename(), calendar.Feed(bookings))
}

// inviteFor renders the invite body for notifications (best-effort).
func (h bookingHandler) inviteFor(r *http.Request, id string) string {
	b, err := h.bookings.Get(r.Context(), id)
	if err != nil {
		return ""
	}
	return calendar.Invite(*b)
}

// bookingError maps booking sentinel errors to HTTP responses.
func bookingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, booking.ErrFacilityNotFound), errors.Is(err, booking.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, booking.ErrNotBookable):
		writeError(w, http.StatusConflict, "that time is not available")
	case errors.Is(err, booking.ErrForbidden):
		writeError(w, http.StatusForbidden, "not permitted")
	case errors.Is(err, booking.ErrBadState):
		writeError(w, http.StatusConflict, "action not valid for this booking")
	case errors.Is(err, booking.ErrNotModifiable):
		writeError(w, http.StatusConflict, "this booking can no longer be changed")
	default:
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
}
