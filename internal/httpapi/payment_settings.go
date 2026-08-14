package httpapi

import (
	"errors"
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/payment"
)

type paymentSettingsHandler struct {
	svc *payment.SettingsService
}

// get returns the available payment modules plus the current selection, so the
// admin form renders from the registry rather than a hardcoded list.
func (h paymentSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	set, err := h.svc.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load payment settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modules":  payment.Modules(),
		"settings": set,
	})
}

type paymentSettingsReq struct {
	Kind   payment.Kind      `json:"kind"`
	Config map[string]string `json:"config"`
}

// update records the municipality's chosen payment module (admin only).
func (h paymentSettingsHandler) update(w http.ResponseWriter, r *http.Request) {
	actor := auth.FromContext(r.Context())
	var req paymentSettingsReq
	if !decode(w, r, &req) {
		return
	}
	set, err := h.svc.Set(r.Context(), req.Kind, req.Config, *actor)
	if err != nil {
		paymentSettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modules":  payment.Modules(),
		"settings": set,
	})
}

func paymentSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payment.ErrUnknownKind),
		errors.Is(err, payment.ErrUnknownField),
		errors.Is(err, payment.ErrMissingField):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not save payment settings")
	}
}
