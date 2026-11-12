package pfif

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseLegacy12(t *testing.T) {
	f, err := os.Open("../../examples/person-finder-1.2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != "1.2" {
		t.Errorf("version = %q", doc.Version)
	}
	if len(doc.Persons) != 2 || len(doc.Notes) != 0 {
		t.Fatalf("persons=%d top-level notes=%d", len(doc.Persons), len(doc.Notes))
	}
	p := doc.Persons[0]
	if p.Name() != "Daniel Okafor" || p.GivenName != "Daniel" || p.HomePostalCode != "92501" {
		t.Errorf("legacy fields not mapped: %+v", p)
	}
	if !strings.Contains(p.Description, "walker") {
		t.Errorf("<other> not folded into description: %q", p.Description)
	}
	if len(p.Notes) != 1 || p.Notes[0].PersonRecordID != p.PersonRecordID || p.Notes[0].Status != "information_sought" {
		t.Errorf("nested note: %+v", p.Notes)
	}
	// The top-level note is attached to its person.
	mei := doc.Persons[1]
	if len(mei.Notes) != 1 || !mei.Notes[0].AuthorMadeContact || mei.Notes[0].Status != "believed_alive" {
		t.Errorf("top-level note not attached: %+v", mei.Notes)
	}
	want := time.Date(2026, 11, 10, 8, 15, 0, 0, time.UTC)
	if !p.EntryDate.Equal(want) {
		t.Errorf("entry date %v", p.EntryDate)
	}
}

func TestRoundTrip(t *testing.T) {
	in := Person{
		PersonRecordID: "example.org/person.1",
		EntryDate:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		FullName:       "Zoë <Tester> & Co",
		AuthorPhone:    "+15550100",
		HomeCity:       "Riverside",
		Notes: []Note{{
			NoteRecordID:      "example.org/note.1",
			PersonRecordID:    "example.org/person.1",
			EntryDate:         time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC),
			Status:            "is_note_author",
			AuthorMadeContact: true,
			Text:              "Line one\nLine \"two\"",
		}},
	}
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WritePerson(&in); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteExpired("example.org/person.gone", in.EntryDate); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `xmlns:pfif="http://zesty.ca/pfif/1.4"`) || !strings.Contains(out, "Zoë &lt;Tester&gt; &amp; Co") {
		t.Fatalf("unexpected XML:\n%s", out)
	}
	// PFIF requires a fixed element order: person_record_id before entry_date
	// before full_name before notes.
	order := []string{"person_record_id", "entry_date", "author_phone", "full_name", "home_city", "<pfif:note>"}
	last := -1
	for _, el := range order {
		i := strings.Index(out, el)
		if i < last {
			t.Errorf("%s is out of order", el)
		}
		last = i
	}

	doc, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != "1.4" || len(doc.Persons) != 2 {
		t.Fatalf("version %q persons %d", doc.Version, len(doc.Persons))
	}
	got := doc.Persons[0]
	if got.FullName != in.FullName || got.AuthorPhone != in.AuthorPhone || !got.EntryDate.Equal(in.EntryDate) {
		t.Errorf("person changed in round trip: %+v", got)
	}
	if len(got.Notes) != 1 || got.Notes[0].Text != in.Notes[0].Text || !got.Notes[0].AuthorMadeContact {
		t.Errorf("note changed in round trip: %+v", got.Notes)
	}
	if !doc.Persons[1].Expired(time.Now()) {
		t.Error("tombstone should be expired")
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		``,
		`<html></html>`,
		`<pfif:pfif xmlns:pfif="http://example.com/other"></pfif:pfif>`,
		`<pfif:pfif xmlns:pfif="http://zesty.ca/pfif/1.4"><pfif:person>`,
	} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
	_, err := Parse(strings.NewReader(`<root/>`))
	if !errors.Is(err, ErrNotPFIF) {
		t.Errorf("want ErrNotPFIF, got %v", err)
	}
}

func TestParseEntityExpansionIsNotPossible(t *testing.T) {
	// encoding/xml does not expand custom entities, so "billion laughs"
	// documents fail instead of exhausting memory.
	bomb := `<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;">]>
<pfif:pfif xmlns:pfif="http://zesty.ca/pfif/1.4"><pfif:person><pfif:full_name>&b;</pfif:full_name></pfif:person></pfif:pfif>`
	if _, err := Parse(strings.NewReader(bomb)); err == nil {
		t.Error("custom entities should be rejected")
	}
}

func TestParseTime(t *testing.T) {
	for _, v := range []string{"2026-11-12T10:00:00Z", "2026-11-12T10:00:00", "2026-11-12T10:00:00.000Z", "2026-11-12T12:00:00+02:00"} {
		got := ParseTime(v)
		if !got.Equal(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("ParseTime(%q) = %v", v, got)
		}
	}
	if !ParseTime("garbage").IsZero() {
		t.Error("garbage should give zero time")
	}
	if FormatTime(time.Date(2026, 1, 2, 3, 4, 5, 999, time.FixedZone("x", 3600))) != "2026-01-02T02:04:05Z" {
		t.Error("FormatTime must output UTC")
	}
}
