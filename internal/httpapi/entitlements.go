package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
)

type entitlementHandler struct {
	svc *entitlement.Service
}

// mine returns the caller's currently-resolved entitlements. Resolving here also
// silently re-validates against the stored provider reference, so a returning
// holder proves nothing again.
func (h entitlementHandler) mine(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, h.svc.Resolve(r.Context(), *user))
}

// descriptor publishes what the provider needs from the applicant, so the
// proving form renders from the provider's contract rather than from rules
// hardcoded in the SPA.
func (h entitlementHandler) descriptor(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Describe(entitlement.Type(chi.URLParam(r, "type")))
	if err != nil {
		entitlementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type proveReq struct {
	Inputs map[string]string `json:"inputs"`
}

// prove submits the applicant's inputs for evaluation. Note what this endpoint
// does NOT do: it never accepts an outcome. The body carries evidence for the
// provider; the provider decides. This is the replacement for the old
// POST /verify-residency, which took an address and set the flag.
func (h entitlementHandler) prove(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req proveReq
	if !decode(w, r, &req) {
		return
	}
	det, err := h.svc.Prove(r.Context(), *user, entitlement.Type(chi.URLParam(r, "type")), req.Inputs)
	if err != nil {
		entitlementError(w, err)
		return
	}
	// Report the fresh picture rather than the single determination, so the
	// client cannot infer anything from a bare outcome it did not ask for.
	writeJSON(w, http.StatusOK, map[string]any{
		"outcome":      det.Outcome,
		"entitlements": h.svc.Resolve(r.Context(), *user),
	})
}

func entitlementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entitlement.ErrUnsupportedType):
		writeError(w, http.StatusNotFound, "no provider is configured for that entitlement")
	case errors.Is(err, entitlement.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, entitlement.ErrUnreachable):
		// 503, not 403: the check could not be made, which is not a refusal.
		writeError(w, http.StatusServiceUnavailable, "the verification service is unavailable; normal rates apply for now")
	default:
		writeError(w, http.StatusInternalServerError, "could not evaluate the entitlement")
	}
}
