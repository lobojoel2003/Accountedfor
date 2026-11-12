package sms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lobojoel2003/accountedfor/internal/sms"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/testdb"
)

func TestServiceFlow(t *testing.T) {
	st := testdb.Open(t, "sms")
	ctx := context.Background()
	svc := &sms.Service{Store: st, SiteName: "Board", BaseURL: "https://b.example", DefaultVisibility: store.Limited}
	send := func(from, body string) string {
		t.Helper()
		reply, err := svc.Handle(ctx, from, body)
		if err != nil {
			t.Fatal(err)
		}
		if len([]rune(reply)) > sms.MaxReply {
			t.Fatalf("reply too long: %d", len([]rune(reply)))
		}
		return reply
	}

	if r := send("+1 555 0100", "MISSING Maria Lopez, last seen at the bridge"); !strings.Contains(r, "Maria Lopez") {
		t.Errorf("missing reply: %q", r)
	}
	r := send("+15550111", "SAFE María López & Juan Lopez, at the stadium")
	if !strings.Contains(r, "María López, Juan Lopez") || !strings.Contains(r, "1 report") {
		t.Errorf("safe reply: %q", r)
	}
	if r := send("+15550112", "seguro privado Rosa Díaz"); !strings.Contains(r, "Solo equipos") {
		t.Errorf("Spanish private reply: %q", r)
	}
	r = send("+15550113", "FIND lopez")
	if !strings.Contains(r, "SAFE") || !strings.Contains(r, "BEING LOOKED FOR") {
		t.Errorf("find reply: %q", r)
	}
	if r := send("+15550113", "FIND rosa diaz"); !strings.Contains(r, "No one found") {
		t.Errorf("private record found by SMS search: %q", r)
	}
	if r := send("+15550111", "DELETE"); !strings.Contains(r, "2 posts") {
		t.Errorf("delete reply: %q", r)
	}
	if r := send("+15550111", "STOP"); r != "" {
		t.Errorf("STOP must not reply, got %q", r)
	}
	if r := send("+15550111", "what"); !strings.Contains(r, "Text SAFE") {
		t.Errorf("help reply: %q", r)
	}

	people, _, err := st.List(ctx, store.ListOptions{Origin: store.OriginSMS})
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Fatalf("expected Maria (missing) and Rosa to remain, got %d", len(people))
	}
	for _, p := range people {
		if p.AuthorPhone == "" || p.AuthorPhone[0] != '+' {
			t.Errorf("phone not normalized: %q", p.AuthorPhone)
		}
	}
}

func TestRateLimit(t *testing.T) {
	st := testdb.Open(t, "sms")
	calls := 0
	svc := &sms.Service{Store: st, SiteName: "Board", DefaultVisibility: store.Limited,
		Allow: func(string) bool { calls++; return calls <= 1 }}
	if r, _ := svc.Handle(context.Background(), "+1", "HELP"); strings.Contains(r, "Too many") {
		t.Fatal("first message limited")
	}
	if r, _ := svc.Handle(context.Background(), "+1", "HELP"); !strings.Contains(r, "Too many") {
		t.Fatalf("second message not limited: %q", r)
	}
}
