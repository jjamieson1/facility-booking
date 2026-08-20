package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/users"
)

type userAdminHandler struct {
	svc *users.Service
}

// list returns the current administrators/staff plus any pending invites.
func (h userAdminHandler) list(w http.ResponseWriter, r *http.Request) {
	elevated, err := h.svc.ListElevated(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	invites, err := h.svc.ListInvites(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list invites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": elevated, "invites": invites})
}

type inviteReq struct {
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
}

// invite promotes an existing user or records a pending grant for a new email.
func (h userAdminHandler) invite(w http.ResponseWriter, r *http.Request) {
	actor := auth.FromContext(r.Context())
	var req inviteReq
	if !decode(w, r, &req) {
		return
	}
	res, err := h.svc.Invite(r.Context(), req.Email, req.Role, *actor)
	if err != nil {
		userError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type roleReq struct {
	Role domain.Role `json:"role"`
}

// setRole changes an existing user's role (promote or revoke).
func (h userAdminHandler) setRole(w http.ResponseWriter, r *http.Request) {
	actor := auth.FromContext(r.Context())
	var req roleReq
	if !decode(w, r, &req) {
		return
	}
	u, err := h.svc.SetRole(r.Context(), chi.URLParam(r, "id"), req.Role, *actor)
	if err != nil {
		userError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// revokeInvite deletes a pending grant.
func (h userAdminHandler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	actor := auth.FromContext(r.Context())
	if err := h.svc.RevokeInvite(r.Context(), chi.URLParam(r, "id"), *actor); err != nil {
		userError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userError maps the users service's sentinel errors to HTTP status codes.
func userError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, users.ErrInvalidEmail), errors.Is(err, users.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, users.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, users.ErrCannotDemoteSelf), errors.Is(err, users.ErrLastAdmin):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not update user")
	}
}
