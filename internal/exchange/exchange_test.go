package exchange_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/exchange"
	"github.com/lobojoel2003/accountedfor/internal/pfif"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/testdb"
)

func TestImportExample(t *testing.T) {
	st := testdb.Open(t, "exchange")
	ctx := context.Background()
	f, err := os.Open("../../examples/person-finder-1.2.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	res, err := exchange.Import(ctx, st, f, exchange.ImportOptions{Visibility: store.Public, Retention: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 2 || res.Notes != 2 || res.Skipped != 0 {
		t.Fatalf("result: %+v", res)
	}
	p, err := st.GetPersonByRecordID(ctx, "example.personfinder.org/person.1002")
	if err != nil {
		t.Fatal(err)
	}
	if p.FullName != "Mei Chen" || p.Status != "safe" || p.Origin != store.OriginImport || p.ExpiryDate == nil {
		t.Errorf("imported person: %+v", p)
	}

	// Importing again updates instead of duplicating.
	f2, _ := os.Open("../../examples/person-finder-1.2.xml")
	defer f2.Close()
	res, err = exchange.Import(ctx, st, f2, exchange.ImportOptions{})
	if err != nil || res.Created != 0 || res.Updated != 2 || res.Notes != 0 {
		t.Fatalf("re-import: %+v %v", res, err)
	}
}

func TestImportValidationAndDeletion(t *testing.T) {
	st := testdb.Open(t, "exchange")
	ctx := context.Background()
	doc := `<pfif:pfif xmlns:pfif="http://zesty.ca/pfif/1.4">
  <pfif:person><pfif:person_record_id>no-slash</pfif:person_record_id><pfif:full_name>X</pfif:full_name></pfif:person>
  <pfif:person><pfif:person_record_id>a.org/person.1</pfif:person_record_id></pfif:person>
  <pfif:person><pfif:person_record_id>a.org/person.2</pfif:person_record_id><pfif:full_name>Kept</pfif:full_name>
    <pfif:photo_url>javascript:alert(1)</pfif:photo_url>
    <pfif:note><pfif:note_record_id>a.org/note.1</pfif:note_record_id><pfif:status>bogus</pfif:status><pfif:text>hi</pfif:text></pfif:note>
  </pfif:person>
  <pfif:note><pfif:note_record_id>a.org/note.2</pfif:note_record_id><pfif:person_record_id>a.org/person.404</pfif:person_record_id></pfif:note>
</pfif:pfif>`
	res, err := exchange.Import(ctx, st, strings.NewReader(doc), exchange.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Skipped != 3 || len(res.Problems) != 3 {
		t.Fatalf("result: %+v", res)
	}
	p, err := st.GetPersonByRecordID(ctx, "a.org/person.2")
	if err != nil {
		t.Fatal(err)
	}
	if p.PhotoURL != "" || p.Visibility != store.Limited {
		t.Errorf("unsafe URL kept or wrong default visibility: %+v", p)
	}

	// An expired copy of the record deletes it here.
	del := `<pfif:pfif xmlns:pfif="http://zesty.ca/pfif/1.4"><pfif:person>
  <pfif:person_record_id>a.org/person.2</pfif:person_record_id>
  <pfif:expiry_date>2020-01-01T00:00:00Z</pfif:expiry_date></pfif:person></pfif:pfif>`
	res, err = exchange.Import(ctx, st, strings.NewReader(del), exchange.ImportOptions{})
	if err != nil || res.Deleted != 1 {
		t.Fatalf("delete: %+v %v", res, err)
	}
}

func TestExportLevels(t *testing.T) {
	st := testdb.Open(t, "exchange")
	ctx := context.Background()
	mk := func(name string, vis store.Visibility, notes ...store.Note) *store.Person {
		p := &store.Person{FullName: name, Visibility: vis, Origin: store.OriginWeb,
			AuthorPhone: "+15550100", AuthorEmail: "a@example.org", HomeCity: "Riverside", Age: "40"}
		if err := st.CreatePerson(ctx, p, notes, ""); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mk("Public Person", store.Public,
		store.Note{Status: store.StatusIsNoteAuthor, Text: "public text", AuthorPhone: "+15550101", Origin: store.OriginWeb},
		store.Note{Text: "private note text", Private: true, Origin: store.OriginWeb})
	mk("Limited Person", store.Limited, store.Note{Status: store.StatusIsNoteAuthor, Text: "limited text", Origin: store.OriginWeb})
	mk("Private Person", store.Private, store.Note{Status: store.StatusIsNoteAuthor, Origin: store.OriginWeb})
	mk("Deceased Person", store.Public, store.Note{Status: store.StatusBelievedDead, Text: "sad", Origin: store.OriginResponder})
	gone := mk("Deleted Person", store.Public)
	if err := st.DeletePerson(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	var pub bytes.Buffer
	n, err := exchange.Export(ctx, st, &pub, exchange.ExportOptions{Level: exchange.Public, Tombstones: true})
	if err != nil {
		t.Fatal(err)
	}
	out := pub.String()
	if n != 3 {
		t.Errorf("public export has %d records", n)
	}
	for _, leak := range []string{"+1555", "a@example.org", "Private Person", "private note text", "limited text", "believed_dead"} {
		if strings.Contains(out, leak) {
			t.Errorf("public export leaks %q", leak)
		}
	}
	for _, want := range []string{"Public Person", "public text", "Limited Person", gone.RecordID} {
		if !strings.Contains(out, want) {
			t.Errorf("public export lacks %q", want)
		}
	}
	doc, err := pfif.Parse(&pub)
	if err != nil {
		t.Fatalf("public export is not valid PFIF: %v", err)
	}
	for _, p := range doc.Persons {
		if p.FullName == "Limited Person" && (p.Age != "" || p.HomeCity != "") {
			t.Errorf("limited record details exported: %+v", p)
		}
	}

	var full bytes.Buffer
	n, err = exchange.Export(ctx, st, &full, exchange.ExportOptions{Level: exchange.Full})
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || !strings.Contains(full.String(), "+15550101") || !strings.Contains(full.String(), "believed_dead") {
		t.Errorf("full export incomplete (%d records)", n)
	}

	// Full export can be imported into another server unchanged.
	st2 := testdb.Open(t, "exchange_copy")
	res, err := exchange.Import(ctx, st2, &full, exchange.ImportOptions{})
	if err != nil || res.Created != 4 {
		t.Fatalf("round trip import: %+v %v", res, err)
	}
}
