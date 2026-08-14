package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jjamieson1/facility-booking/internal/payment"
)

type paymentHandler struct {
	svc *payment.Service
}

// maxPageSize caps a client-supplied page size so one request can't pull the
// whole ledger.
const maxPageSize = 100

// ledger returns the payment reconciliation view — window totals plus one page of
// transactions (charges, declines, refunds) with their booking context.
// Query params (all optional):
//   - from, to: YYYY-MM-DD, inclusive, local time — scope the window
//   - filter:   all | succeeded | failed | refunds — narrow the list (not the totals)
//   - page:     0-based page index
//   - pageSize: rows per page (clamped to [1, 100], default 25)
//
// Staff/admin only.
func (h paymentHandler) ledger(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := payment.Params{
		Window:   payment.Window{From: parseDay(q.Get("from")), To: parseDay(q.Get("to"))},
		Filter:   q.Get("filter"),
		Page:     atoiOr(q.Get("page"), 0),
		PageSize: clamp(atoiOr(q.Get("pageSize"), 25), 1, maxPageSize),
	}
	rec, err := h.svc.Reconcile(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build reconciliation")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// atoiOr parses s as an int, returning def when blank or malformed.
func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// parseDay reads a YYYY-MM-DD date in local time; a blank or malformed value
// yields the zero time (an unbounded edge of the window).
func parseDay(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}
	}
	return d
}
