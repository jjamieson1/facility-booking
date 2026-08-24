package httpapi

import (
	"errors"
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/payment"
)

type paymentSettingsHandler struct {
	svc      *payment.SettingsService
	payments *payment.Service
}

// obligations lists refunds owed but not yet issued — money this app could not
// return itself because the gateway takes refund instructions from an operator,
// not from us. Staff-readable: it is a work queue, and someone has to work it.
func (h paymentSettingsHandler) obligations(w http.ResponseWriter, r *http.Request) {
	status := domain.RefundObligationStatus(r.URL.Query().Get("status"))
	if status == "" {
		status = domain.RefundOwed // the queue, not the archive
	}
	out, err := h.payments.Obligations(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load refund obligations")
		return
	}
	writeJSON(w, http.StatusOK, out)
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
