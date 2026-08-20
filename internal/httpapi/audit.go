package httpapi

import (
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
)

type auditHandler struct {
	svc auditlog.Recorder
}

// auditResponse tells the SPA whether the central audit service is wired up, and
// carries the recent entries when it is.
type auditResponse struct {
	Enabled bool             `json:"enabled"`
	Entries []auditlog.Entry `json:"entries"`
}

// log returns recent staff audit events from the central audit-logging service
// (staff/admin only).
func (h auditHandler) log(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || !h.svc.Enabled() {
		writeJSON(w, http.StatusOK, auditResponse{Enabled: false, Entries: []auditlog.Entry{}})
		return
	}
	entries, err := h.svc.List(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusBadGateway, "audit service unavailable")
		return
	}
	if entries == nil {
		entries = []auditlog.Entry{}
	}
	writeJSON(w, http.StatusOK, auditResponse{Enabled: true, Entries: entries})
}
