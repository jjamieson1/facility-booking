package httpapi

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

type languageHandler struct{ db *gorm.DB }

type languageReq struct {
	Language string `json:"language"`
}

// setLanguage stores the caller's preferred language.
//
// The SPA's header toggle only changes the browser's UI. Notifications are sent
// from the server, often days later and to a different device entirely, so the
// preference has to be persisted or every notification is English regardless of
// what the resident chose (§4.11 requires both official languages).
func (h languageHandler) setLanguage(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req languageReq
	if !decode(w, r, &req) {
		return
	}
	// Only the two official languages are accepted. An unknown value is a
	// client bug, and silently storing it would send that person nothing they
	// can read.
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang != "en" && lang != "fr" {
		writeError(w, http.StatusBadRequest, "language must be \"en\" or \"fr\"")
		return
	}
	if err := h.db.WithContext(r.Context()).Model(&domain.User{}).
		Where("id = ?", user.ID).Update("language", lang).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the language preference")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"language": lang})
}
