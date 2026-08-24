package httpapi

import (
	"net/http"
	"strings"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// requestLanguage decides which language to serve content in, most specific
// first:
//
//  1. ?lang= — an explicit request, and what the SPA sends as it follows its own
//     header toggle. An anonymous visitor switching to French has nowhere else
//     to express it.
//  2. the signed-in user's stored preference.
//  3. Accept-Language, for API clients that never touch the SPA.
//  4. English.
//
// The user's preference deliberately loses to an explicit ?lang=: someone
// reading a page in one language should get that page in that language, not
// have a stored setting override what they just asked for.
func requestLanguage(r *http.Request) domain.Language {
	if q := strings.TrimSpace(r.URL.Query().Get("lang")); q != "" {
		return domain.NormalizeLanguage(q)
	}
	if u := auth.FromContext(r.Context()); u != nil && u.Language != "" {
		return domain.NormalizeLanguage(u.Language)
	}
	if h := r.Header.Get("Accept-Language"); h != "" {
		// Take the first tag; full q-value negotiation buys nothing when there
		// are exactly two languages to choose between.
		if first, _, _ := strings.Cut(h, ","); strings.TrimSpace(first) != "" {
			return domain.NormalizeLanguage(first)
		}
	}
	return domain.DefaultLanguage
}
