// Package calendar renders bookings as iCalendar (.ics) data: a single-event
// invite for the booker and a whole-facility feed the city can subscribe to.
package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/jjamieson1/facility-booking/internal/brand"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// productID is the PRODID a calendar client shows as the source of the event.
// Slashes are stripped because PRODID's own syntax uses them as separators, so
// a municipality named "Ville de Saint-Jean/Richelieu" would otherwise produce a
// malformed identifier.
func productID() string {
	return "-//" + strings.ReplaceAll(brand.Name(), "/", " ") + "//Facility Booking//EN"
}

// deaccent maps the accented letters French uses onto ASCII. Dropping them
// instead would turn "Municipalité d'Été" into "municipalit-dt", which is
// unreadable — and this app ships bilingual, so accented municipality names are
// the expected case, not an edge one.
var deaccent = map[rune]rune{
	'à': 'a', 'â': 'a', 'ä': 'a', 'á': 'a', 'ã': 'a', 'å': 'a',
	'ç': 'c',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'î': 'i', 'ï': 'i', 'í': 'i', 'ì': 'i',
	'ô': 'o', 'ö': 'o', 'ò': 'o', 'ó': 'o', 'õ': 'o',
	'ù': 'u', 'û': 'u', 'ü': 'u', 'ú': 'u',
	'ÿ': 'y', 'ñ': 'n',
}

// FeedFilename is what the city calendar feed downloads as.
//
// Reduced to lowercase ASCII and hyphens because it lands in a
// Content-Disposition header and then on someone's filesystem: a quote or a
// path separator surviving into it is a header-injection bug, not a cosmetic one.
func FeedFilename() string {
	slug := strings.ToLower(strings.TrimSpace(brand.Name()))
	slug = strings.Map(func(r rune) rune {
		if ascii, ok := deaccent[r]; ok {
			r = ascii
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		// Word boundaries become hyphens. A slash matters here: dropping it
		// would merge "Saint-Jean/Richelieu" into "jeanrichelieu", running two
		// distinct place names together.
		case r == ' ' || r == '-' || r == '_' || r == '\'' || r == '/' || r == '.' || r == ',':
			return '-'
		default:
			return -1
		}
	}, slug)
	// Collapse runs left by punctuation so "Saint-Jean/Richelieu" does not
	// become "saint-jean--richelieu".
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "facility-bookings"
	}
	return slug + ".ics"
}

// Invite renders one confirmed booking as a complete VCALENDAR the booker can
// add to their calendar. A cancelled booking is emitted with STATUS:CANCELLED so
// importing it withdraws the event.
func Invite(b domain.Booking) string {
	var sb strings.Builder
	writeHeader(&sb)
	writeEvent(&sb, b, true) // the booker's own invite may carry the purpose
	writeFooter(&sb)
	return sb.String()
}

// Feed renders many bookings as one subscribable calendar (the city's view).
// The purpose is omitted here: the feed is public and unauthenticated, and the
// purpose is free text a resident entered that can contain personal detail. Only
// the space, time, and status are exposed.
func Feed(bookings []domain.Booking) string {
	var sb strings.Builder
	writeHeader(&sb)
	for _, b := range bookings {
		writeEvent(&sb, b, false)
	}
	writeFooter(&sb)
	return sb.String()
}

func writeHeader(sb *strings.Builder) {
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	fmt.Fprintf(sb, "PRODID:%s\r\n", productID())
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString("METHOD:PUBLISH\r\n")
}

func writeFooter(sb *strings.Builder) {
	sb.WriteString("END:VCALENDAR\r\n")
}

func writeEvent(sb *strings.Builder, b domain.Booking, includePurpose bool) {
	name := "Facility booking"
	location := ""
	if b.Facility != nil {
		name = b.Facility.Name
		location = b.Facility.Location
	}
	status := "CONFIRMED"
	if b.Status == domain.StatusCancelled || b.Status == domain.StatusDenied {
		status = "CANCELLED"
	}

	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(sb, "UID:%s@rivermont-spaces\r\n", b.ID)
	fmt.Fprintf(sb, "DTSTAMP:%s\r\n", utc(time.Now()))
	fmt.Fprintf(sb, "DTSTART:%s\r\n", utc(b.StartsAt))
	fmt.Fprintf(sb, "DTEND:%s\r\n", utc(b.EndsAt))
	fmt.Fprintf(sb, "SUMMARY:%s\r\n", escape(name))
	if location != "" {
		fmt.Fprintf(sb, "LOCATION:%s\r\n", escape(location))
	}
	if includePurpose && b.Purpose != "" {
		fmt.Fprintf(sb, "DESCRIPTION:%s\r\n", escape(b.Purpose))
	}
	fmt.Fprintf(sb, "STATUS:%s\r\n", status)
	sb.WriteString("END:VEVENT\r\n")
}

func utc(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// escape applies RFC 5545 text escaping for the fields we emit.
func escape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n")
	return r.Replace(s)
}
