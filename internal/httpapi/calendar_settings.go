package httpapi

import (
	"errors"
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/calendar"
)

type calendarSettingsHandler struct {
	svc *calendar.Service
}

// get returns the available calendar modules plus the current selection, so the
// admin form can render the choices without hardcoding them in the SPA.
func (h calendarSettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	set, err := h.svc.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load calendar settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modules":  calendar.Modules(),
		"settings": set,
	})
}

type calendarSettingsReq struct {
	Kind   calendar.Kind     `json:"kind"`
	Config map[string]string `json:"config"`
}

// update records the municipality's chosen calendar module (admin only).
func (h calendarSettingsHandler) update(w http.ResponseWriter, r *http.Request) {
	actor := auth.FromContext(r.Context())
	var req calendarSettingsReq
	if !decode(w, r, &req) {
		return
	}
	set, err := h.svc.Set(r.Context(), req.Kind, req.Config, *actor)
	if err != nil {
		calendarSettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modules":  calendar.Modules(),
		"settings": set,
	})
}

// calendarSettingsError maps the calendar service's sentinel errors to statuses.
func calendarSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, calendar.ErrUnknownKind),
		errors.Is(err, calendar.ErrUnknownField),
		errors.Is(err, calendar.ErrMissingField):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not save calendar settings")
	}
}
