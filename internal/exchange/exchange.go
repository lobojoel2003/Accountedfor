// Package exchange moves records between the store and PFIF documents, so
// Accountedfor can import from and export to other person-finder systems.
package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/pfif"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/textutil"
)

// Level selects how much of each record an export contains.
type Level int

const (
	// Public exports only what the public web pages show: no contact
	// details, no private records or notes, and only name and status for
	// "name only" records.
	Public Level = iota
	// Full exports everything, for trusted partners and responders.
	Full
)

// ExportOptions configures Export.
type ExportOptions struct {
	Since time.Time
	Level Level
	// Tombstones adds placeholders for records deleted since Since.
	Tombstones bool
}

// Export writes matching records as a PFIF 1.4 document and returns the
// number of person records written.
func Export(ctx context.Context, st *store.Store, w io.Writer, opt ExportOptions) (int, error) {
	pw := pfif.NewWriter(w)
	n := 0
	err := st.Each(ctx, store.ExportFilter{
		Since:          opt.Since,
		IncludePrivate: opt.Level == Full,
	}, func(p *store.Person) error {
		rec := ToPFIF(p, opt.Level)
		n++
		return pw.WritePerson(&rec)
	})
	if err != nil {
		return n, err
	}
	if opt.Tombstones {
		ts, err := st.Tombstones(ctx, opt.Since)
		if err != nil {
			return n, err
		}
		for _, t := range ts {
			if err := pw.WriteExpired(t.RecordID, t.DeletedAt); err != nil {
				return n, err
			}
		}
	}
	return n, pw.Close()
}

// ToPFIF converts a stored person (with notes loaded) to PFIF, removing
// whatever the level does not allow.
func ToPFIF(p *store.Person, level Level) pfif.Person {
	out := pfif.Person{
		PersonRecordID: p.RecordID,
		EntryDate:      p.EntryDate,
		FullName:       p.FullName,
		GivenName:      p.GivenName,
		FamilyName:     p.FamilyName,
		AlternateNames: p.AlternateNames,
	}
	if p.ExpiryDate != nil {
		out.ExpiryDate = *p.ExpiryDate
	}
	limited := level == Public && p.Visibility != store.Public
	if !limited {
		out.AuthorName = p.AuthorName
		out.SourceName = p.SourceName
		out.SourceURL = p.SourceURL
		if p.SourceDate != nil {
			out.SourceDate = *p.SourceDate
		}
		out.Description = p.Description
		out.Sex = p.Sex
		out.DateOfBirth = p.DateOfBirth
		out.Age = p.Age
		out.HomeNeighborhood = p.HomeNeighborhood
		out.HomeCity = p.HomeCity
		out.HomeState = p.HomeState
		out.HomeCountry = p.HomeCountry
		out.PhotoURL = p.PhotoURL
		out.ProfileURLs = p.ProfileURLs
	}
	if level == Full {
		out.AuthorEmail = p.AuthorEmail
		out.AuthorPhone = p.AuthorPhone
		out.HomeStreet = p.HomeStreet
		out.HomePostalCode = p.HomePostalCode
	}
	if out.SourceName == "" && store.IsLocalRecord(p.RecordID) && !limited {
		out.SourceName = store.Domain
	}
	for _, n := range p.Notes {
		if n.Hidden || (level == Public && n.Private) {
			continue
		}
		// Public feeds never announce a death; that is for responders and
		// families to communicate in person.
		if level == Public && n.Status == store.StatusBelievedDead {
			continue
		}
		on := pfif.Note{
			NoteRecordID:         n.RecordID,
			PersonRecordID:       p.RecordID,
			LinkedPersonRecordID: n.LinkedPersonRecordID,
			EntryDate:            n.EntryDate,
			AuthorMadeContact:    n.AuthorMadeContact,
			Status:               n.Status,
		}
		if n.SourceDate != nil {
			on.SourceDate = *n.SourceDate
		}
		if !limited {
			on.AuthorName = n.AuthorName
			on.LastKnownLocation = n.LastKnownLocation
			on.Text = n.Text
			on.PhotoURL = n.PhotoURL
		}
		if level == Full {
			on.AuthorEmail = n.AuthorEmail
			on.AuthorPhone = n.AuthorPhone
			on.EmailOfFoundPerson = n.EmailOfFoundPerson
			on.PhoneOfFoundPerson = n.PhoneOfFoundPerson
		}
		out.Notes = append(out.Notes, on)
	}
	return out
}

// ImportOptions configures Import.
type ImportOptions struct {
	// Visibility given to records that are new to this server.
	Visibility store.Visibility
	// Retention sets an expiry date on records that have none. Zero keeps
	// them until deleted.
	Retention time.Duration
	// Now is the current time; tests set it.
	Now time.Time
}

// ImportResult summarizes an import.
type ImportResult struct {
	Version  string
	Created  int
	Updated  int
	Deleted  int
	Notes    int
	Skipped  int
	Problems []string
}

func (r *ImportResult) problem(format string, args ...any) {
	r.Skipped++
	if len(r.Problems) < 25 {
		r.Problems = append(r.Problems, fmt.Sprintf(format, args...))
	}
}

// Summary returns a one-line description of the result.
func (r *ImportResult) Summary() string {
	return fmt.Sprintf("PFIF %s: %d created, %d updated, %d deleted, %d notes, %d skipped",
		r.Version, r.Created, r.Updated, r.Deleted, r.Notes, r.Skipped)
}

// Import reads a PFIF document and stores its records.
func Import(ctx context.Context, st *store.Store, r io.Reader, opt ImportOptions) (*ImportResult, error) {
	doc, err := pfif.Parse(r)
	if err != nil {
		return nil, err
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	if !opt.Visibility.Valid() {
		opt.Visibility = store.Limited
	}
	res := &ImportResult{Version: doc.Version}
	for i := range doc.Persons {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		importPerson(ctx, st, &doc.Persons[i], opt, res)
	}
	// Notes about persons that already exist here.
	byPerson := map[string][]pfif.Note{}
	for _, n := range doc.Notes {
		byPerson[n.PersonRecordID] = append(byPerson[n.PersonRecordID], n)
	}
	for recordID, notes := range byPerson {
		p, err := st.GetPersonByRecordID(ctx, recordID)
		if errors.Is(err, store.ErrNotFound) {
			res.problem("%d note(s) for unknown person %q", len(notes), recordID)
			continue
		}
		if err != nil {
			return res, err
		}
		converted := convertNotes(notes, res)
		added, err := st.AppendNotes(ctx, p.ID, converted)
		if err != nil {
			return res, err
		}
		res.Notes += added
	}
	return res, nil
}

func validRecordID(id string) bool {
	domain, rest, ok := strings.Cut(id, "/")
	return ok && domain != "" && rest != "" && !strings.ContainsAny(id, " \t\n") && len(id) <= 500
}

func importPerson(ctx context.Context, st *store.Store, in *pfif.Person, opt ImportOptions, res *ImportResult) {
	id := strings.TrimSpace(in.PersonRecordID)
	if !validRecordID(id) {
		res.problem("person with invalid person_record_id %q", id)
		return
	}
	if in.Expired(opt.Now) {
		existed, err := st.DeleteByRecordID(ctx, id)
		if err != nil {
			res.problem("%s: delete failed: %v", id, err)
			return
		}
		if existed {
			res.Deleted++
		}
		return
	}
	name := textutil.Line(in.Name(), 200)
	if name == "" {
		res.problem("%s: no full_name or given_name/family_name", id)
		return
	}
	p := &store.Person{
		RecordID:         id,
		FullName:         name,
		GivenName:        textutil.Line(in.GivenName, 100),
		FamilyName:       textutil.Line(in.FamilyName, 100),
		AlternateNames:   textutil.Line(in.AlternateNames, 300),
		Description:      textutil.Multiline(in.Description, 2000),
		Sex:              normalizeSex(in.Sex),
		DateOfBirth:      textutil.Line(in.DateOfBirth, 20),
		Age:              textutil.Line(in.Age, 20),
		HomeStreet:       textutil.Line(in.HomeStreet, 200),
		HomeNeighborhood: textutil.Line(in.HomeNeighborhood, 200),
		HomeCity:         textutil.Line(in.HomeCity, 200),
		HomeState:        textutil.Line(in.HomeState, 200),
		HomePostalCode:   textutil.Line(in.HomePostalCode, 20),
		HomeCountry:      textutil.Line(in.HomeCountry, 100),
		PhotoURL:         safeURL(in.PhotoURL),
		ProfileURLs:      textutil.Multiline(in.ProfileURLs, 2000),
		AuthorName:       textutil.Line(in.AuthorName, 200),
		AuthorEmail:      textutil.Line(in.AuthorEmail, 200),
		AuthorPhone:      textutil.Line(in.AuthorPhone, 40),
		SourceName:       textutil.Line(in.SourceName, 200),
		SourceURL:        safeURL(in.SourceURL),
		EntryDate:        in.EntryDate,
		Visibility:       opt.Visibility,
		Origin:           store.OriginImport,
	}
	if !in.SourceDate.IsZero() {
		t := in.SourceDate
		p.SourceDate = &t
	}
	if p.EntryDate.IsZero() || p.EntryDate.After(opt.Now) {
		p.EntryDate = opt.Now
	}
	if !in.ExpiryDate.IsZero() {
		t := in.ExpiryDate
		p.ExpiryDate = &t
	} else if opt.Retention > 0 {
		t := opt.Now.Add(opt.Retention)
		p.ExpiryDate = &t
	}
	notes := convertNotes(in.Notes, res)
	created, added, err := st.UpsertImported(ctx, p, notes)
	switch {
	case errors.Is(err, store.ErrTombstoned):
		res.problem("%s: was deleted on this server; not re-imported", id)
		return
	case err != nil:
		res.problem("%s: %v", id, err)
		return
	case created:
		res.Created++
	default:
		res.Updated++
	}
	res.Notes += added
}

func convertNotes(in []pfif.Note, res *ImportResult) []store.Note {
	out := make([]store.Note, 0, len(in))
	for _, n := range in {
		id := strings.TrimSpace(n.NoteRecordID)
		if !validRecordID(id) {
			res.problem("note with invalid note_record_id %q", id)
			continue
		}
		status := strings.TrimSpace(n.Status)
		if !store.ValidNoteStatus(status) {
			status = store.StatusNone
		}
		sn := store.Note{
			RecordID:             id,
			LinkedPersonRecordID: strings.TrimSpace(n.LinkedPersonRecordID),
			Status:               status,
			AuthorMadeContact:    n.AuthorMadeContact,
			AuthorName:           textutil.Line(n.AuthorName, 200),
			AuthorEmail:          textutil.Line(n.AuthorEmail, 200),
			AuthorPhone:          textutil.Line(n.AuthorPhone, 40),
			EmailOfFoundPerson:   textutil.Line(n.EmailOfFoundPerson, 200),
			PhoneOfFoundPerson:   textutil.Line(n.PhoneOfFoundPerson, 40),
			LastKnownLocation:    textutil.Line(n.LastKnownLocation, 300),
			Text:                 textutil.Multiline(n.Text, 3000),
			PhotoURL:             safeURL(n.PhotoURL),
			Origin:               store.OriginImport,
			EntryDate:            n.EntryDate,
		}
		if !validRecordID(sn.LinkedPersonRecordID) {
			sn.LinkedPersonRecordID = ""
		}
		if !n.SourceDate.IsZero() {
			t := n.SourceDate
			sn.SourceDate = &t
		}
		out = append(out, sn)
	}
	return out
}

func normalizeSex(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "female", "f":
		return "female"
	case "male", "m":
		return "male"
	case "other":
		return "other"
	}
	return ""
}

// safeURL keeps only http(s) URLs so imported data cannot inject
// javascript: links.
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	l := strings.ToLower(u)
	if strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
		return textutil.Line(u, 1000)
	}
	return ""
}
