// Package randid generates unguessable identifiers and secrets.
package randid

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// alphabet avoids characters that are easy to confuse when read aloud or
// copied by hand (0/o, 1/l/i).
const alphabet = "23456789abcdefghjkmnpqrstuvwxyz"

// PublicID returns a 10 character identifier (about 49 bits of entropy) used
// in public URLs. It is short enough to read over a phone line yet not
// enumerable.
func PublicID() string { return fromAlphabet(10) }

// Short returns an n character identifier from the same readable alphabet.
func Short(n int) string { return fromAlphabet(n) }

func fromAlphabet(n int) string {
	b := make([]byte, n)
	// Rejection sampling keeps the distribution uniform.
	max := byte(256 - 256%len(alphabet))
	buf := make([]byte, n*2)
	i := 0
	for i < n {
		if _, err := rand.Read(buf); err != nil {
			panic("randid: crypto/rand failed: " + err.Error())
		}
		for _, c := range buf {
			if c >= max {
				continue
			}
			b[i] = alphabet[int(c)%len(alphabet)]
			i++
			if i == n {
				break
			}
		}
	}
	return string(b)
}

var tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Token returns a 32 character secret with 160 bits of entropy.
func Token() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic("randid: crypto/rand failed: " + err.Error())
	}
	return strings.ToLower(tokenEncoding.EncodeToString(b))
}

// Hash returns the SHA-256 of a secret. Only hashes of secrets are stored.
func Hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
