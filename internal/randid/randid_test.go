package randid

import (
	"bytes"
	"strings"
	"testing"
)

func TestPublicID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := PublicID()
		if len(id) != 10 {
			t.Fatalf("len(%q) = %d", id, len(id))
		}
		for _, c := range id {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("unexpected character %q in %q", c, id)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestTokenAndHash(t *testing.T) {
	a, b := Token(), Token()
	if len(a) != 32 || a == b {
		t.Fatalf("bad tokens %q %q", a, b)
	}
	if !bytes.Equal(Hash(a), Hash(a)) || bytes.Equal(Hash(a), Hash(b)) {
		t.Fatal("hash is not deterministic or collides")
	}
}
