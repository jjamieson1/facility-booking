package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/media"
	"github.com/jjamieson1/facility-booking/internal/waiver"
)

type waiverHandler struct {
	svc *waiver.Service
	// onUploaded re-checks a conditional booking after a document arrives —
	// uploading may have been the last thing outstanding (§4.5). A func rather
	// than the booking handler itself, so this package's upload path does not
	// grow a dependency on approvals.
	onUploaded func(r *http.Request, bookingID string)
}

// upload accepts a multipart "file" waiver/insurance doc for the caller's
// booking. The body is capped; media.Save decides the type from the bytes and
// re-encodes images (see internal/media).
func (h waiverHandler) upload(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads not configured")
		return
	}
	user := auth.FromContext(r.Context())
	// Cap the whole request body before multipart parsing (defense in depth).
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+1024)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a file upload")
		return
	}
	defer file.Close()

	b, err := h.svc.Upload(r.Context(), user, chi.URLParam(r, "id"), file)
	switch {
	case errors.Is(err, waiver.ErrNotFound):
		writeError(w, http.StatusNotFound, "booking not found")
	case errors.Is(err, waiver.ErrForbidden):
		writeError(w, http.StatusForbidden, "not your booking")
	case errors.Is(err, media.ErrUnsupported):
		writeError(w, http.StatusBadRequest, "unsupported file type — upload a PDF, PNG, JPEG, or GIF")
	case errors.Is(err, media.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file too large (max 10 MB)")
	case errors.Is(err, media.ErrImageTooBig), errors.Is(err, media.ErrDecodeFailed):
		writeError(w, http.StatusBadRequest, "could not process that image")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "upload failed")
	default:
		if h.onUploaded != nil {
			h.onUploaded(r, chi.URLParam(r, "id"))
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// download streams a booking's waiver to its owner or staff, with headers that
// stop a browser from ever treating the bytes as active content.
func (h waiverHandler) download(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads not configured")
		return
	}
	user := auth.FromContext(r.Context())
	rc, ctype, err := h.svc.Open(r.Context(), user, chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, waiver.ErrForbidden):
		writeError(w, http.StatusForbidden, "not permitted")
		return
	case err != nil:
		writeError(w, http.StatusNotFound, "no waiver on file")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "attachment; filename=\"waiver\"")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}
