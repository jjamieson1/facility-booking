package notify

import (
	"fmt"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// message is one notification's text, in the recipient's language. Short is used
// for SMS and must stand alone: a citizen who reads only the SMS should still
// know what happened and to which booking.
type message struct {
	Title string
	Body  string
	Short string
}

// lang normalises a stored preference to a language we have copy for. Canada
// requires both official languages (§4.11); anything unrecognised falls back to
// English rather than sending an empty message.
func lang(pref string) string {
	if len(pref) >= 2 && (pref[:2] == "fr" || pref[:2] == "FR") {
		return "fr"
	}
	return "en"
}

// when renders a booking time for a person, in their language. Times are shown
// in the server's local zone, which is the municipality's — a resident booking a
// municipal hall is in that zone.
func when(t time.Time, l string) string {
	if l == "fr" {
		return t.Format("2006-01-02 à 15:04")
	}
	return t.Format("Mon 2 Jan 2006, 3:04 PM")
}

func facilityName(b domain.Booking) string {
	if b.Facility != nil && b.Facility.Name != "" {
		return b.Facility.Name
	}
	return "the facility"
}

// bookingSubmitted tells the booker their request is with staff.
func bookingSubmitted(b domain.Booking, l string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		return message{
			Title: "Demande de réservation reçue",
			Body: fmt.Sprintf("Votre demande pour %s le %s a été reçue et attend l'approbation du personnel. "+
				"Vous serez avisé dès qu'elle sera examinée.", name, at),
			Short: fmt.Sprintf("Demande reçue : %s, %s. En attente d'approbation.", name, at),
		}
	}
	return message{
		Title: "Booking request received",
		Body: fmt.Sprintf("Your request for %s on %s has been received and is waiting for staff approval. "+
			"We'll let you know as soon as it's been reviewed.", name, at),
		Short: fmt.Sprintf("Request received: %s, %s. Awaiting approval.", name, at),
	}
}

// staffReviewNeeded tells a staff member a request is waiting.
func staffReviewNeeded(b domain.Booking, l string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		return message{
			Title: "Réservation à approuver",
			Body:  fmt.Sprintf("Une demande pour %s le %s attend votre examen dans le back-office.", name, at),
			Short: fmt.Sprintf("À approuver : %s, %s.", name, at),
		}
	}
	return message{
		Title: "Booking needs approval",
		Body:  fmt.Sprintf("A request for %s on %s is waiting for your review in the back-office.", name, at),
		Short: fmt.Sprintf("Needs approval: %s, %s.", name, at),
	}
}

// bookingConfirmed carries the calendar link. C2's notification API takes no
// attachments, so the .ics is a link to this app rather than a file — the
// booker must be signed in to fetch it, which is correct for a document naming
// their booking.
func bookingConfirmed(b domain.Booking, l, inviteURL string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		m := message{
			Title: "Réservation confirmée",
			Body:  fmt.Sprintf("Votre réservation de %s le %s est confirmée.", name, at),
			Short: fmt.Sprintf("Confirmé : %s, %s.", name, at),
		}
		if inviteURL != "" {
			m.Body += fmt.Sprintf(" Ajoutez-la à votre agenda : %s", inviteURL)
		}
		return m
	}
	m := message{
		Title: "Booking confirmed",
		Body:  fmt.Sprintf("Your booking of %s on %s is confirmed.", name, at),
		Short: fmt.Sprintf("Confirmed: %s, %s.", name, at),
	}
	if inviteURL != "" {
		m.Body += fmt.Sprintf(" Add it to your calendar: %s", inviteURL)
	}
	return m
}

func bookingDenied(b domain.Booking, l string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		return message{
			Title: "Demande de réservation refusée",
			Body: fmt.Sprintf("Votre demande pour %s le %s n'a pas été approuvée. "+
				"Communiquez avec la Ville si vous souhaitez en connaître la raison ou proposer une autre date.", name, at),
			Short: fmt.Sprintf("Refusée : %s, %s.", name, at),
		}
	}
	return message{
		Title: "Booking request declined",
		Body: fmt.Sprintf("Your request for %s on %s was not approved. "+
			"Contact the city if you'd like to know why or to propose another time.", name, at),
		Short: fmt.Sprintf("Declined: %s, %s.", name, at),
	}
}

func bookingCancelled(b domain.Booking, l string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		return message{
			Title: "Réservation annulée",
			Body: fmt.Sprintf("Votre réservation de %s le %s a été annulée et le créneau est de nouveau disponible. "+
				"Tout remboursement applicable suit les conditions d'annulation de l'installation.", name, at),
			Short: fmt.Sprintf("Annulée : %s, %s.", name, at),
		}
	}
	return message{
		Title: "Booking cancelled",
		Body: fmt.Sprintf("Your booking of %s on %s has been cancelled and the slot is free again. "+
			"Any refund follows the facility's cancellation terms.", name, at),
		Short: fmt.Sprintf("Cancelled: %s, %s.", name, at),
	}
}

// bookingReminder carries the before-use instructions, which is the whole point
// of the reminder (§4.10) — keys, access codes, setup.
func bookingReminder(b domain.Booking, l, instructions string) message {
	name, at := facilityName(b), when(b.StartsAt, l)
	if l == "fr" {
		m := message{
			Title: "Rappel : votre réservation approche",
			Body:  fmt.Sprintf("Rappel de votre réservation de %s le %s.", name, at),
			Short: fmt.Sprintf("Rappel : %s, %s.", name, at),
		}
		if instructions != "" {
			m.Body += "\n\nAvant votre arrivée : " + instructions
		}
		return m
	}
	m := message{
		Title: "Reminder: your booking is coming up",
		Body:  fmt.Sprintf("A reminder about your booking of %s on %s.", name, at),
		Short: fmt.Sprintf("Reminder: %s, %s.", name, at),
	}
	if instructions != "" {
		m.Body += "\n\nBefore you arrive: " + instructions
	}
	return m
}

// waitlistOpened tells someone a slot they wanted has freed up. Deliberately
// does not promise the slot — anyone can book it, and this is a nudge, not a
// hold.
func waitlistOpened(e domain.WaitlistEntry, facility, l, bookURL string) message {
	at := when(e.StartsAt, l)
	if l == "fr" {
		m := message{
			Title: "Un créneau s'est libéré",
			Body: fmt.Sprintf("Le créneau de %s le %s, pour lequel vous étiez sur la liste d'attente, est de nouveau libre. "+
				"Il est offert au premier arrivé.", facility, at),
			Short: fmt.Sprintf("Libre : %s, %s.", facility, at),
		}
		if bookURL != "" {
			m.Body += " Réservez ici : " + bookURL
		}
		return m
	}
	m := message{
		Title: "A slot you wanted is free",
		Body: fmt.Sprintf("The %s slot on %s that you joined the waitlist for is free again. "+
			"It's open to whoever books it first.", facility, at),
		Short: fmt.Sprintf("Free: %s, %s.", facility, at),
	}
	if bookURL != "" {
		m.Body += " Book it here: " + bookURL
	}
	return m
}
