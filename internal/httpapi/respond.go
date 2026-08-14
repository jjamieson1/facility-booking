package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
)

// writeJSON encodes v as JSON with the given status. Encoding errors are logged
// implicitly by the failed write; the header is already sent by then.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends a safe, structured error message — never internal detail.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode reads a JSON request body into dst, writing a 400 and returning false
// on malformed input. Bodies are capped to guard against oversized payloads.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20)) // 1 MiB
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// writeICS streams an iCalendar document as a downloadable attachment.
func writeICS(w http.ResponseWriter, filename, body string) {
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
