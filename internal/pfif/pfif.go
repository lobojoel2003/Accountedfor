// Package pfif reads and writes the People Finder Interchange Format (PFIF),
// the XML format used by Google Person Finder and other missing-person
// systems to exchange records.
//
// Writing always produces PFIF 1.4. Reading accepts PFIF 1.1 through 1.4,
// including the older field names (first_name, last_name, home_zip, found)
// and notes placed either inside or after their person element.
//
// Specification: https://web.archive.org/web/2023/https://zesty.ca/pfif/1.4/
package pfif

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Namespaces of the PFIF versions this package understands.
const (
	NS11 = "http://zesty.ca/pfif/1.1"
	NS12 = "http://zesty.ca/pfif/1.2"
	NS13 = "http://zesty.ca/pfif/1.3"
	NS14 = "http://zesty.ca/pfif/1.4"
)

var versions = map[string]string{NS11: "1.1", NS12: "1.2", NS13: "1.3", NS14: "1.4"}

// Note statuses defined by PFIF 1.4.
var Statuses = []string{"information_sought", "is_note_author", "believed_alive", "believed_missing", "believed_dead"}

// Person is a PFIF person record.
type Person struct {
	PersonRecordID   string
	EntryDate        time.Time
	ExpiryDate       time.Time
	AuthorName       string
	AuthorEmail      string
	AuthorPhone      string
	SourceName       string
	SourceDate       time.Time
	SourceURL        string
	FullName         string
	GivenName        string
	FamilyName       string
	AlternateNames   string
	Description      string
	Sex              string
	DateOfBirth      string
	Age              string
	HomeStreet       string
	HomeNeighborhood string
	HomeCity         string
	HomeState        string
	HomePostalCode   string
	HomeCountry      string
	PhotoURL         string
	ProfileURLs      string
	Notes            []Note
}

// Name returns FullName, or the given and family names joined.
func (p *Person) Name() string {
	if strings.TrimSpace(p.FullName) != "" {
		return strings.TrimSpace(p.FullName)
	}
	return strings.TrimSpace(strings.TrimSpace(p.GivenName) + " " + strings.TrimSpace(p.FamilyName))
}

// Expired reports whether the record carries an expiry date before now.
// Such records are deletion notices.
func (p *Person) Expired(now time.Time) bool {
	return !p.ExpiryDate.IsZero() && p.ExpiryDate.Before(now)
}

// Note is a PFIF note record.
type Note struct {
	NoteRecordID         string
	PersonRecordID       string
	LinkedPersonRecordID string
	EntryDate            time.Time
	AuthorName           string
	AuthorEmail          string
	AuthorPhone          string
	SourceDate           time.Time
	AuthorMadeContact    bool
	Status               string
	EmailOfFoundPerson   string
	PhoneOfFoundPerson   string
	LastKnownLocation    string
	Text                 string
	PhotoURL             string
}

// Document is a parsed PFIF file.
type Document struct {
	// Version is the PFIF version of the input, e.g. "1.4".
	Version string
	Persons []Person
	// Notes holds top-level notes. Notes that belong to a person in the same
	// document are attached to it instead.
	Notes []Note
}

// MaxFieldLength bounds every field read from untrusted input.
const MaxFieldLength = 10000

// ErrNotPFIF is returned when the root element is not a PFIF document.
var ErrNotPFIF = errors.New("not a PFIF document (expected a <pfif> root element in a zesty.ca/pfif namespace)")

// Parse reads a PFIF document.
func Parse(r io.Reader) (*Document, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = true
	doc := &Document{}

	var (
		inRoot bool
		person *Person
		note   *Note
		field  string
		text   strings.Builder
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse PFIF: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if !inRoot {
				if name != "pfif" || (t.Name.Space != "" && versions[t.Name.Space] == "") {
					return nil, ErrNotPFIF
				}
				inRoot = true
				doc.Version = versions[t.Name.Space]
				if doc.Version == "" {
					doc.Version = "1.4"
				}
				continue
			}
			switch {
			case name == "person" && person == nil && note == nil:
				person = &Person{}
			case name == "note" && note == nil:
				note = &Note{}
			default:
				field = name
				text.Reset()
			}
		case xml.CharData:
			if field != "" && text.Len() < MaxFieldLength*4 {
				text.Write(t)
			}
		case xml.EndElement:
			name := t.Name.Local
			switch {
			case field != "" && name == field:
				value := clamp(strings.TrimSpace(text.String()))
				if note != nil {
					setNote(note, field, value)
				} else if person != nil {
					setPerson(person, field, value)
				}
				field = ""
			case name == "note" && note != nil:
				if person != nil {
					if note.PersonRecordID == "" {
						note.PersonRecordID = person.PersonRecordID
					}
					person.Notes = append(person.Notes, *note)
				} else {
					doc.Notes = append(doc.Notes, *note)
				}
				note = nil
			case name == "person" && person != nil:
				for i := range person.Notes {
					if person.Notes[i].PersonRecordID == "" {
						person.Notes[i].PersonRecordID = person.PersonRecordID
					}
				}
				doc.Persons = append(doc.Persons, *person)
				person = nil
			}
		}
	}
	if !inRoot {
		return nil, ErrNotPFIF
	}

	// Attach top-level notes to persons in the same document.
	if len(doc.Notes) > 0 {
		index := make(map[string]int, len(doc.Persons))
		for i, p := range doc.Persons {
			index[p.PersonRecordID] = i
		}
		rest := doc.Notes[:0]
		for _, n := range doc.Notes {
			if i, ok := index[n.PersonRecordID]; ok {
				doc.Persons[i].Notes = append(doc.Persons[i].Notes, n)
			} else {
				rest = append(rest, n)
			}
		}
		doc.Notes = rest
	}
	return doc, nil
}

func clamp(s string) string {
	if len(s) <= MaxFieldLength {
		return s
	}
	r := []rune(s)
	if len(r) > MaxFieldLength {
		r = r[:MaxFieldLength]
	}
	return string(r)
}

func setPerson(p *Person, field, v string) {
	switch field {
	case "person_record_id":
		p.PersonRecordID = v
	case "entry_date":
		p.EntryDate = ParseTime(v)
	case "expiry_date":
		p.ExpiryDate = ParseTime(v)
	case "author_name":
		p.AuthorName = v
	case "author_email":
		p.AuthorEmail = v
	case "author_phone":
		p.AuthorPhone = v
	case "source_name":
		p.SourceName = v
	case "source_date":
		p.SourceDate = ParseTime(v)
	case "source_url":
		p.SourceURL = v
	case "full_name":
		p.FullName = v
	case "given_name", "first_name":
		p.GivenName = v
	case "family_name", "last_name":
		p.FamilyName = v
	case "alternate_names":
		p.AlternateNames = v
	case "description":
		p.Description = v
	case "other":
		// PFIF 1.1/1.2 free-text field, folded into description.
		if p.Description != "" {
			p.Description += "\n"
		}
		p.Description += v
	case "sex":
		p.Sex = v
	case "date_of_birth":
		p.DateOfBirth = v
	case "age":
		p.Age = v
	case "home_street":
		p.HomeStreet = v
	case "home_neighborhood":
		p.HomeNeighborhood = v
	case "home_city":
		p.HomeCity = v
	case "home_state":
		p.HomeState = v
	case "home_postal_code", "home_zip":
		p.HomePostalCode = v
	case "home_country":
		p.HomeCountry = v
	case "photo_url":
		p.PhotoURL = v
	case "profile_urls":
		p.ProfileURLs = v
	}
}

func setNote(n *Note, field, v string) {
	switch field {
	case "note_record_id":
		n.NoteRecordID = v
	case "person_record_id":
		n.PersonRecordID = v
	case "linked_person_record_id":
		n.LinkedPersonRecordID = v
	case "entry_date":
		n.EntryDate = ParseTime(v)
	case "author_name":
		n.AuthorName = v
	case "author_email":
		n.AuthorEmail = v
	case "author_phone":
		n.AuthorPhone = v
	case "source_date":
		n.SourceDate = ParseTime(v)
	case "author_made_contact", "found":
		n.AuthorMadeContact = strings.EqualFold(v, "true") || v == "1"
	case "status":
		n.Status = v
	case "email_of_found_person":
		n.EmailOfFoundPerson = v
	case "phone_of_found_person":
		n.PhoneOfFoundPerson = v
	case "last_known_location":
		n.LastKnownLocation = v
	case "text":
		n.Text = v
	case "photo_url":
		n.PhotoURL = v
	}
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04Z",
	"2006-01-02",
}

// ParseTime parses a PFIF timestamp leniently. Timestamps without a zone are
// UTC, as PFIF requires. It returns the zero time if v cannot be parsed.
func ParseTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// FormatTime formats t as PFIF requires: UTC with second precision.
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// Writer streams a PFIF 1.4 document.
type Writer struct {
	w      *bufio.Writer
	err    error
	closed bool
}

// NewWriter writes the XML declaration and opening root element.
func NewWriter(w io.Writer) *Writer {
	pw := &Writer{w: bufio.NewWriterSize(w, 32*1024)}
	pw.raw(xml.Header)
	pw.raw(`<pfif:pfif xmlns:pfif="` + NS14 + `">` + "\n")
	return pw
}

func (w *Writer) raw(s string) {
	if w.err == nil {
		_, w.err = w.w.WriteString(s)
	}
}

func (w *Writer) elem(indent, name, value string) {
	if value == "" {
		return
	}
	w.raw(indent + "<pfif:" + name + ">")
	if w.err == nil {
		w.err = xml.EscapeText(w.w, []byte(value))
	}
	w.raw("</pfif:" + name + ">\n")
}

func (w *Writer) date(indent, name string, t time.Time) {
	if !t.IsZero() {
		w.elem(indent, name, FormatTime(t))
	}
}

// WritePerson writes a person and its notes.
func (w *Writer) WritePerson(p *Person) error {
	const in = "    "
	w.raw("  <pfif:person>\n")
	w.elem(in, "person_record_id", p.PersonRecordID)
	w.date(in, "entry_date", p.EntryDate)
	w.date(in, "expiry_date", p.ExpiryDate)
	w.elem(in, "author_name", p.AuthorName)
	w.elem(in, "author_email", p.AuthorEmail)
	w.elem(in, "author_phone", p.AuthorPhone)
	w.elem(in, "source_name", p.SourceName)
	w.date(in, "source_date", p.SourceDate)
	w.elem(in, "source_url", p.SourceURL)
	w.elem(in, "full_name", p.FullName)
	w.elem(in, "given_name", p.GivenName)
	w.elem(in, "family_name", p.FamilyName)
	w.elem(in, "alternate_names", p.AlternateNames)
	w.elem(in, "description", p.Description)
	w.elem(in, "sex", p.Sex)
	w.elem(in, "date_of_birth", p.DateOfBirth)
	w.elem(in, "age", p.Age)
	w.elem(in, "home_street", p.HomeStreet)
	w.elem(in, "home_neighborhood", p.HomeNeighborhood)
	w.elem(in, "home_city", p.HomeCity)
	w.elem(in, "home_state", p.HomeState)
	w.elem(in, "home_postal_code", p.HomePostalCode)
	w.elem(in, "home_country", p.HomeCountry)
	w.elem(in, "photo_url", p.PhotoURL)
	w.elem(in, "profile_urls", p.ProfileURLs)
	for i := range p.Notes {
		w.writeNote(&p.Notes[i], "    ")
	}
	w.raw("  </pfif:person>\n")
	return w.err
}

func (w *Writer) writeNote(n *Note, indent string) {
	in := indent + "  "
	w.raw(indent + "<pfif:note>\n")
	w.elem(in, "note_record_id", n.NoteRecordID)
	w.elem(in, "person_record_id", n.PersonRecordID)
	w.elem(in, "linked_person_record_id", n.LinkedPersonRecordID)
	w.date(in, "entry_date", n.EntryDate)
	w.elem(in, "author_name", n.AuthorName)
	w.elem(in, "author_email", n.AuthorEmail)
	w.elem(in, "author_phone", n.AuthorPhone)
	w.date(in, "source_date", n.SourceDate)
	if n.AuthorMadeContact {
		w.elem(in, "author_made_contact", "true")
	} else {
		w.elem(in, "author_made_contact", "false")
	}
	w.elem(in, "status", n.Status)
	w.elem(in, "email_of_found_person", n.EmailOfFoundPerson)
	w.elem(in, "phone_of_found_person", n.PhoneOfFoundPerson)
	w.elem(in, "last_known_location", n.LastKnownLocation)
	w.elem(in, "text", n.Text)
	w.elem(in, "photo_url", n.PhotoURL)
	w.raw(indent + "</pfif:note>\n")
}

// WriteExpired writes a placeholder for a deleted or expired record so that
// consumers delete their copy, as PFIF 1.3+ recommends.
func (w *Writer) WriteExpired(recordID string, deletedAt time.Time) error {
	const in = "    "
	w.raw("  <pfif:person>\n")
	w.elem(in, "person_record_id", recordID)
	w.date(in, "entry_date", deletedAt)
	w.date(in, "expiry_date", deletedAt)
	w.raw("  </pfif:person>\n")
	return w.err
}

// Close writes the closing root element and flushes. It does not close the
// underlying writer.
func (w *Writer) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	w.raw("</pfif:pfif>\n")
	if w.err == nil {
		w.err = w.w.Flush()
	}
	return w.err
}
