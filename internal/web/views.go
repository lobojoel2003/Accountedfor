package web

import (
	"github.com/lobojoel2003/accountedfor/internal/store"
)

// PersonView is a person as one particular viewer may see it. All privacy
// decisions for HTML pages are made here, in one place.
type PersonView struct {
	P *store.Person
	// Full is true for signed-in responders.
	Full bool
	// ShowDetails allows descriptions, ages, home areas and note texts.
	ShowDetails bool
	// Category is safe, missing, unknown, deceased (responders only) or
	// contact (what the public sees instead of deceased).
	Category  string
	BadgeKey  string
	StatusKey string
	// Message is a one-line latest message for lists.
	Message string
	Notes   []NoteView
}

// NoteView is a note as a particular viewer may see it.
type NoteView struct {
	N         store.Note
	ShowText  bool
	Category  string
	StatusKey string
	Linked    *store.Person
}

// canSee reports whether a viewer may open a record at all.
func canSee(p *store.Person, responder bool) bool {
	if responder {
		return true
	}
	return !p.Hidden && p.Visibility != store.Private
}

func newPersonView(p *store.Person, responder bool) PersonView {
	v := PersonView{
		P:           p,
		Full:        responder,
		ShowDetails: responder || p.Visibility == store.Public,
	}
	v.Category, v.BadgeKey, v.StatusKey = statusLabels(p.PFIFStatus, responder)
	return v
}

// statusLabels maps a PFIF status to a category and translation keys. The
// public is never told that someone has died; they are asked to contact
// responders instead.
func statusLabels(pfifStatus string, responder bool) (category, badgeKey, statusKey string) {
	category = store.Category(pfifStatus)
	if category == "deceased" && !responder {
		return "contact", "badge.contact", "status.contact_responders"
	}
	statusKey = "status.none"
	if pfifStatus != "" {
		statusKey = "status." + pfifStatus
	}
	return category, "badge." + category, statusKey
}

// withNotes attaches note views. linked maps linked record IDs to people
// that the viewer is allowed to see.
func (v *PersonView) withNotes(linked map[string]*store.Person) {
	for _, n := range v.P.Notes {
		if !v.Full && n.Hidden {
			continue
		}
		nv := NoteView{
			N: n,
			// The public never reads the details of a death report; they are
			// told to contact responders instead.
			ShowText: v.Full || (v.ShowDetails && !n.Private && n.Status != store.StatusBelievedDead),
		}
		nv.Category, _, nv.StatusKey = statusLabels(n.Status, v.Full)
		if n.Status == "" {
			nv.StatusKey = "status.note"
		}
		if lp := linked[n.LinkedPersonRecordID]; lp != nil && canSee(lp, v.Full) {
			nv.Linked = lp
		}
		v.Notes = append(v.Notes, nv)
	}
}

// listMessage returns a one-line message for lists, respecting privacy.
func listMessage(p *store.Person, n *store.Note, responder bool) string {
	if n == nil {
		return ""
	}
	if !responder && (p.Visibility != store.Public || n.Private) {
		return ""
	}
	msg := n.Text
	if n.LastKnownLocation != "" {
		if msg != "" {
			msg = n.LastKnownLocation + " — " + msg
		} else {
			msg = n.LastKnownLocation
		}
	}
	r := []rune(msg)
	if len(r) > 140 {
		msg = string(r[:139]) + "…"
	}
	return msg
}
