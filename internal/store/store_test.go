package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/testdb"
)

func newPerson(name string, vis store.Visibility, status string) (*store.Person, []store.Note) {
	return &store.Person{FullName: name, Visibility: vis, Origin: store.OriginWeb},
		[]store.Note{{Status: status, Text: "note for " + name, Origin: store.OriginWeb}}
}

func create(t *testing.T, st *store.Store, name string, vis store.Visibility, status string) *store.Person {
	t.Helper()
	p, notes := newPerson(name, vis, status)
	if err := st.CreatePerson(context.Background(), p, notes, "secret-key"); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateAndStatus(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	p := create(t, st, "María López", store.Limited, store.StatusInformationSought)
	if p.ID == 0 || len(p.PublicID) != 10 || p.RecordID != "test.example.org/person."+p.PublicID {
		t.Fatalf("identifiers not set: %+v", p)
	}
	got, err := st.GetPerson(ctx, p.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "missing" || got.PFIFStatus != store.StatusInformationSought {
		t.Fatalf("status = %q/%q", got.Status, got.PFIFStatus)
	}

	// A newer note changes the status; hiding it changes it back.
	n := &store.Note{Status: store.StatusBelievedAlive, Origin: store.OriginWeb}
	if err := st.AddNote(ctx, p.ID, n); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetPerson(ctx, p.PublicID)
	if got.Status != "safe" {
		t.Fatalf("status after note = %q", got.Status)
	}
	if _, err := st.SetNoteHidden(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetPerson(ctx, p.PublicID)
	if got.Status != "missing" {
		t.Fatalf("status after hiding note = %q", got.Status)
	}
	if err := st.LoadNotes(ctx, got, false); err != nil || len(got.Notes) != 1 {
		t.Fatalf("visible notes = %d, %v", len(got.Notes), err)
	}
	if err := st.LoadNotes(ctx, got, true); err != nil || len(got.Notes) != 2 {
		t.Fatalf("all notes = %d, %v", len(got.Notes), err)
	}
}

func TestEditToken(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	p := create(t, st, "Ana", store.Public, store.StatusIsNoteAuthor)
	if _, err := st.VerifyEditToken(ctx, p.PublicID, "secret-key"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, bad := range []string{"", "wrong", "secret-key "} {
		if _, err := st.VerifyEditToken(ctx, p.PublicID, bad); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("key %q accepted", bad)
		}
	}
}

func TestSearch(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	create(t, st, "María José López García", store.Public, store.StatusIsNoteAuthor)
	create(t, st, "Mario Lopes", store.Limited, store.StatusInformationSought)
	create(t, st, "Secret Lopez", store.Private, store.StatusIsNoteAuthor)
	hidden := create(t, st, "Spam Lopez", store.Public, store.StatusIsNoteAuthor)
	if err := st.SetPersonHidden(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	names := func(people []store.Person) map[string]bool {
		m := map[string]bool{}
		for _, p := range people {
			m[p.FullName] = true
		}
		return m
	}

	pub, err := st.Search(ctx, "lopez", store.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := names(pub)
	if !got["María José López García"] || got["Secret Lopez"] || got["Spam Lopez"] {
		t.Errorf("public search = %v", got)
	}
	// Accent- and case-insensitive, partial and fuzzy.
	for _, q := range []string{"MARIA LOPEZ", "garcia", "maria jose lopez", "mario lopez"} {
		res, err := st.Search(ctx, q, store.SearchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) == 0 {
			t.Errorf("no results for %q", q)
		}
	}
	all, err := st.Search(ctx, "lopez", store.SearchOptions{IncludePrivate: true, IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(all); !got["Secret Lopez"] || !got["Spam Lopez"] {
		t.Errorf("responder search = %v", got)
	}
	missing, err := st.Search(ctx, "lopes", store.SearchOptions{Status: "missing"})
	if err != nil || len(missing) != 1 || missing[0].FullName != "Mario Lopes" {
		t.Errorf("status filter: %v %v", names(missing), err)
	}
}

func TestMatchesAndSimilarMissing(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	m := create(t, st, "Maria Lopez", store.Public, store.StatusInformationSought)
	s := create(t, st, "María López", store.Limited, store.StatusIsNoteAuthor)
	create(t, st, "Juan Lopez", store.Limited, store.StatusIsNoteAuthor)

	sim, err := st.SimilarMissing(ctx, "María López", s.ID, 5)
	if err != nil || len(sim) != 1 || sim[0].ID != m.ID {
		t.Fatalf("SimilarMissing = %v, %v", sim, err)
	}
	if sim, _ := st.SimilarMissing(ctx, "Juan Lopez", 0, 5); len(sim) != 0 {
		t.Errorf("Juan should not match Maria: %v", sim)
	}

	matches, err := st.Matches(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Missing.ID != m.ID || matches[0].Safe.ID != s.ID {
		t.Fatalf("matches = %+v", matches)
	}
	if err := st.DismissMatch(ctx, m.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if matches, _ := st.Matches(ctx, 10); len(matches) != 0 {
		t.Errorf("dismissed match still listed")
	}
}

func TestDeleteLeavesTombstone(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	before := time.Now().Add(-time.Minute)
	p := create(t, st, "Gone Person", store.Public, store.StatusIsNoteAuthor)
	if err := st.DeletePerson(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPerson(ctx, p.PublicID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted record still found: %v", err)
	}
	ts, err := st.Tombstones(ctx, before)
	if err != nil || len(ts) != 1 || ts[0].RecordID != p.RecordID {
		t.Fatalf("tombstones = %v, %v", ts, err)
	}
	// Re-importing a deleted record is refused.
	again := &store.Person{RecordID: p.RecordID, FullName: "Gone Person", Visibility: store.Public}
	if _, _, err := st.UpsertImported(ctx, again, nil); !errors.Is(err, store.ErrTombstoned) {
		t.Fatalf("tombstoned record re-imported: %v", err)
	}
}

func TestPurgeExpired(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	p, notes := newPerson("Expired", store.Public, store.StatusIsNoteAuthor)
	p.ExpiryDate = &past
	if err := st.CreatePerson(ctx, p, notes, ""); err != nil {
		t.Fatal(err)
	}
	keep := create(t, st, "Kept", store.Public, store.StatusIsNoteAuthor)
	n, err := st.PurgeExpired(ctx, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if _, err := st.GetPerson(ctx, keep.PublicID); err != nil {
		t.Fatal("unexpired record purged")
	}
}

func TestSMSDelete(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	for _, name := range []string{"A", "B"} {
		p := &store.Person{FullName: name, Visibility: store.Limited, Origin: store.OriginSMS, AuthorPhone: "+15550100"}
		if err := st.CreatePerson(ctx, p, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	web := &store.Person{FullName: "C", Visibility: store.Limited, Origin: store.OriginWeb, AuthorPhone: "+15550100"}
	if err := st.CreatePerson(ctx, web, nil, ""); err != nil {
		t.Fatal(err)
	}
	n, err := st.DeleteSMSPosts(ctx, "+15550100")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	if _, err := st.GetPerson(ctx, web.PublicID); err != nil {
		t.Fatal("web post must not be deleted by SMS DELETE")
	}
}

func TestEachPagesThroughEverything(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	for i := 0; i < 1203; i++ {
		p := &store.Person{FullName: "Person", Visibility: store.Public, Origin: store.OriginImport}
		if err := st.CreatePerson(ctx, p, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	create(t, st, "Private", store.Private, store.StatusIsNoteAuthor)
	count := func(f store.ExportFilter) int {
		n := 0
		if err := st.Each(ctx, f, func(*store.Person) error { n++; return nil }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(store.ExportFilter{}); n != 1203 {
		t.Errorf("public export visited %d", n)
	}
	if n := count(store.ExportFilter{IncludePrivate: true}); n != 1204 {
		t.Errorf("full export visited %d", n)
	}
	if n := count(store.ExportFilter{Since: time.Now().Add(time.Hour)}); n != 0 {
		t.Errorf("future since visited %d", n)
	}
}

func TestUsersSessionsTokens(t *testing.T) {
	st := testdb.Open(t, "store")
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "Bad Name!", "", "long-enough-password", store.RoleAdmin); err == nil {
		t.Error("invalid username accepted")
	}
	if _, err := st.CreateUser(ctx, "alice", "", "short", store.RoleAdmin); err == nil {
		t.Error("short password accepted")
	}
	u, err := st.CreateUser(ctx, "Alice", "Alice A.", "long-enough-password", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "alice", "", "long-enough-password", store.RoleResponder); !errors.Is(err, store.ErrExists) {
		t.Errorf("duplicate username: %v", err)
	}
	if _, err := st.Authenticate(ctx, "ALICE", "wrong-password-here"); !errors.Is(err, store.ErrNotFound) {
		t.Error("wrong password accepted")
	}
	if _, err := st.Authenticate(ctx, "nobody", "long-enough-password"); !errors.Is(err, store.ErrNotFound) {
		t.Error("unknown user accepted")
	}
	if _, err := st.Authenticate(ctx, "alice", "long-enough-password"); err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(ctx, u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.GetSession(ctx, token)
	if err != nil || sess.User.Username != "alice" || sess.CSRFToken == "" {
		t.Fatalf("session = %+v, %v", sess, err)
	}
	// Disabling signs out.
	if err := st.SetUserDisabled(ctx, "alice", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Error("session survived disabling")
	}
	if _, err := st.Authenticate(ctx, "alice", "long-enough-password"); !errors.Is(err, store.ErrNotFound) {
		t.Error("disabled user can sign in")
	}

	tok, secret, err := st.CreateAPIToken(ctx, "Partner", "read")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.CheckAPIToken(ctx, secret); err != nil || got.ID != tok.ID || got.CanWrite() {
		t.Fatalf("token check: %+v %v", got, err)
	}
	if err := st.RevokeAPIToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CheckAPIToken(ctx, secret); !errors.Is(err, store.ErrNotFound) {
		t.Error("revoked token accepted")
	}
}
