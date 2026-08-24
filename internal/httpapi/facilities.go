package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/facility"
)

// errBadWindow is returned when date/start/end search params are malformed.
var errBadWindow = errors.New("date must be YYYY-MM-DD and start/end HH:MM, with end after start")

type facilityHandler struct {
	svc *facility.Service
}

// list returns the directory, optionally filtered by query params (§4.3):
//
//	?minCapacity=50&free=true                          capacity, free-only
//	&accessory=Projector&accessory=Sound+system        repeat for "all of"
//	&area=North+End                                    area/zone
//	&stepFree=true&accessibleWashroom=true             accessibility needs
//	&minFee=0&maxFee=10000                             cost range, in cents
//	&date=2026-07-25&start=14:00&end=17:00             date/time search (§4.4)
//
// All of them AND together, and the date/time window ANDs on top, because §4.3
// specifies that filters combine and all must match.
//
// Note what is *not* read from the query string: whether the viewer is a
// resident. The cost range is compared against the price this viewer would
// actually pay, and that price follows from an entitlement the provider decides
// — accepting ?resident=true would let anyone filter at the resident rate.
// Anonymous visitors are priced as non-residents, matching what the facility
// page already quotes them, so the directory never under-states a cost.
func (h facilityHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user := auth.FromContext(r.Context())
	f := facility.Filter{
		MinCapacity:        atoi(q.Get("minCapacity")),
		FreeOnly:           q.Get("free") == "true",
		Accessories:        q["accessory"],
		Area:               strings.TrimSpace(q.Get("area")),
		StepFree:           q.Get("stepFree") == "true",
		AccessibleWashroom: q.Get("accessibleWashroom") == "true",
		MinFeeCents:        atoi(q.Get("minFee")),
		MaxFeeCents:        atoi(q.Get("maxFee")),
		Resident:           user != nil && user.IsResident,
	}
	from, to, ok, err := parseWindow(q.Get("date"), q.Get("start"), q.Get("end"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		from, to = time.Time{}, time.Time{} // no window → parameter search only
	}
	facilities, err := h.svc.Search(r.Context(), f, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load facilities")
		return
	}
	if facilities == nil {
		facilities = []domain.Facility{} // serialize an empty result as [], not null
	}
	// Serve the content in the reader's language. Translations overlay per field,
	// so a partly-translated facility shows French where it has French.
	refs := make([]*domain.Facility, len(facilities))
	for i := range facilities {
		refs[i] = &facilities[i]
	}
	if _, err := h.svc.Translate(r.Context(), requestLanguage(r), refs...); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load facilities")
		return
	}
	writeJSON(w, http.StatusOK, facilities)
}

// filterOptions lists the areas and accessories the filter panel can offer.
// Public, like the directory it filters: an anonymous resident browsing for a
// space near them needs the options as much as a signed-in one.
func (h facilityHandler) filterOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.svc.FilterOptions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load filter options")
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// parseWindow builds a [from, to] time window from date=YYYY-MM-DD and
// start/end=HH:MM (server-local). ok is false when the window is absent; an
// error is returned only when the values are present but malformed or reversed.
func parseWindow(date, start, end string) (from, to time.Time, ok bool, err error) {
	if date == "" || start == "" || end == "" {
		return time.Time{}, time.Time{}, false, nil
	}
	from, err = time.ParseInLocation("2006-01-02 15:04", date+" "+start, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, false, errBadWindow
	}
	to, err = time.ParseInLocation("2006-01-02 15:04", date+" "+end, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, false, errBadWindow
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, false, errBadWindow
	}
	return from, to, true, nil
}

// facilityResponse is a facility plus which of its fields the reader is seeing
// in the default language because this one has no translation. The SPA marks
// those rather than passing English off as French (§4.11).
type facilityResponse struct {
	*domain.Facility
	Language     domain.Language `json:"language"`
	Untranslated []string        `json:"untranslated,omitempty"`
}

func (h facilityHandler) get(w http.ResponseWriter, r *http.Request) {
	f, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	lang := requestLanguage(r)
	missing, err := h.svc.TranslateOne(r.Context(), lang, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the facility")
		return
	}
	writeJSON(w, http.StatusOK, facilityResponse{Facility: f, Language: lang, Untranslated: missing})
}

// availability returns hourly slots for ?date=YYYY-MM-DD (defaults to today).
func (h facilityHandler) availability(w http.ResponseWriter, r *http.Request) {
	day := time.Now()
	if d := r.URL.Query().Get("date"); d != "" {
		// Local time so slot start times line up with the browser's clock (and the
		// availability calendar, which is also local).
		parsed, err := time.ParseInLocation("2006-01-02", d, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		day = parsed
	}
	slots, err := h.svc.DayAvailability(r.Context(), chi.URLParam(r, "id"), day)
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	writeJSON(w, http.StatusOK, slots)
}

// calendar returns a weekly (or multi-day) availability grid for ?from=YYYY-MM-DD
// (defaults to the Monday of the current week), ?days=7. Public.
func (h facilityHandler) calendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := mondayOf(time.Now())
	if d := q.Get("from"); d != "" {
		parsed, err := time.ParseInLocation("2006-01-02", d, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be YYYY-MM-DD")
			return
		}
		from = parsed
	}
	cal, err := h.svc.Calendar(r.Context(), chi.URLParam(r, "id"), from, atoi(q.Get("days")))
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	writeJSON(w, http.StatusOK, cal)
}

// mondayOf returns the Monday (00:00 local) of the week containing t.
func mondayOf(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // Sunday=0 → 6, Monday=1 → 0
	d := t.AddDate(0, 0, -offset)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
}

func (h facilityHandler) create(w http.ResponseWriter, r *http.Request) {
	var f domain.Facility
	if !decode(w, r, &f) {
		return
	}
	if err := h.svc.Create(r.Context(), &f); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create facility")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (h facilityHandler) update(w http.ResponseWriter, r *http.Request) {
	var in domain.Facility
	if !decode(w, r, &in) {
		return
	}
	f, err := h.svc.Update(r.Context(), chi.URLParam(r, "id"), &in)
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (h facilityHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete facility")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- blackouts (staff) -----------------------------------------------------

func (h facilityHandler) listBlackouts(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListBlackouts(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load blackouts")
		return
	}
	if out == nil {
		out = []domain.Blackout{}
	}
	writeJSON(w, http.StatusOK, out)
}

type blackoutReq struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Reason string    `json:"reason"`
}

func (h facilityHandler) addBlackout(w http.ResponseWriter, r *http.Request) {
	var req blackoutReq
	if !decode(w, r, &req) {
		return
	}
	b, err := h.svc.AddBlackout(r.Context(), chi.URLParam(r, "id"), req.Start, req.End, req.Reason)
	switch {
	case errors.Is(err, facility.ErrNotFound):
		writeError(w, http.StatusNotFound, "facility not found")
	case errors.Is(err, facility.ErrBadRange):
		writeError(w, http.StatusBadRequest, "end must be after start")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not add blackout")
	default:
		writeJSON(w, http.StatusCreated, b)
	}
}

func (h facilityHandler) removeBlackout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RemoveBlackout(r.Context(), chi.URLParam(r, "blackoutId")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove blackout")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// translations returns every language's text for a facility, for the staff
// editor's tabs.
func (h facilityHandler) translations(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Translations(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type translationReq struct {
	Language           string `json:"language"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	BeforeInstructions string `json:"beforeInstructions"`
	AfterInstructions  string `json:"afterInstructions"`
}

// saveTranslation upserts one language's text for a facility (staff).
func (h facilityHandler) saveTranslation(w http.ResponseWriter, r *http.Request) {
	var req translationReq
	if !decode(w, r, &req) {
		return
	}
	// Reject an unservable language rather than storing text nobody will read.
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	if lang != string(domain.LangEN) && lang != string(domain.LangFR) {
		writeError(w, http.StatusBadRequest, "language must be \"en\" or \"fr\"")
		return
	}
	err := h.svc.SaveTranslation(r.Context(), domain.FacilityTranslation{
		FacilityID:         chi.URLParam(r, "id"),
		Language:           domain.Language(lang),
		Name:               strings.TrimSpace(req.Name),
		Description:        strings.TrimSpace(req.Description),
		BeforeInstructions: strings.TrimSpace(req.BeforeInstructions),
		AfterInstructions:  strings.TrimSpace(req.AfterInstructions),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "facility not found")
		return
	}
	out, err := h.svc.Translations(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not reload translations")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
