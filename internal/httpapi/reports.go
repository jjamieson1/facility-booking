package httpapi

import (
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/reports"
)

type reportHandler struct {
	svc *reports.Service
}

// summary returns the utilization + revenue dashboard for ?period=month|quarter|year
// (default year) — staff/finance.
func (h reportHandler) summary(w http.ResponseWriter, r *http.Request) {
	period := reports.Year
	switch r.URL.Query().Get("period") {
	case "month":
		period = reports.Month
	case "quarter":
		period = reports.Quarter
	}
	sum, err := h.svc.Summarize(r.Context(), period)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build report")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}
