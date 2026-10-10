package auth

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func BenchmarkSignUserID(b *testing.B) {
	userID := hex.EncodeToString(randBytes(16))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = signUserID(userID, "test-secret-key")
	}
}

func BenchmarkValidSignature(b *testing.B) {
	secret := "test-secret-key"
	userID := hex.EncodeToString(randBytes(16))
	sig := signUserID(userID, secret)
	// разбираем "uid:sig"
	parts := splitFirst(sig, ':')
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !validSignature(parts[0], parts[1], secret) {
			b.Fatal("invalid")
		}
	}
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func splitFirst(s string, sep byte) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}
