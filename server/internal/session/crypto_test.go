package session

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := deriveKey("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}

	for _, plaintext := range [][]byte{
		{},
		[]byte("hello"),
		bytes.Repeat([]byte("x"), 4096),
	} {
		ciphertext, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%d bytes): %v", len(plaintext), err)
		}
		if bytes.Equal(ciphertext, plaintext) {
			t.Fatalf("ciphertext equals plaintext for %d-byte input", len(plaintext))
		}
		got, err := Decrypt(key, ciphertext)
		if err != nil {
			t.Fatalf("Decrypt(%d bytes): %v", len(plaintext), err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("round trip mismatch for %d-byte input", len(plaintext))
		}
	}
}

func TestDecryptRejectsShortAndTamperedCiphertext(t *testing.T) {
	key, err := deriveKey("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}

	if _, err := Decrypt(key, []byte("short")); err == nil {
		t.Fatal("Decrypt(short ciphertext) = nil, want error")
	}

	ciphertext, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0x01
	if _, err := Decrypt(key, ciphertext); err == nil {
		t.Fatal("Decrypt(tampered ciphertext) = nil, want authentication error")
	}
}

func TestDeriveKeyDeterministicAndSecretScoped(t *testing.T) {
	a1, err := deriveKey("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("deriveKey a1: %v", err)
	}
	a2, err := deriveKey("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("deriveKey a2: %v", err)
	}
	b, err := deriveKey("abcdef0123456789abcdef0123456789")
	if err != nil {
		t.Fatalf("deriveKey b: %v", err)
	}

	if !bytes.Equal(a1, a2) {
		t.Fatal("deriveKey must be deterministic for the same secret")
	}
	if bytes.Equal(a1, b) {
		t.Fatal("deriveKey must produce different keys for different secrets")
	}
	if len(a1) != 32 {
		t.Fatalf("derived key length = %d, want 32", len(a1))
	}
}

func TestSessionIDAndHash(t *testing.T) {
	a, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID a: %v", err)
	}
	b, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID b: %v", err)
	}
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("session id lengths = %d/%d, want 64/64", len(a), len(b))
	}
	if a == b {
		t.Fatal("GenerateSessionID returned duplicate IDs")
	}

	const emptySHA256Hex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := HashSessionID(""); got != emptySHA256Hex {
		t.Fatalf("HashSessionID(empty) = %q, want %q", got, emptySHA256Hex)
	}
}

func TestManagerRejectsWeakSecretAndDecryptsTokens(t *testing.T) {
	if _, err := NewManager(nil, "", false, slog.Default()); err == nil {
		t.Fatal("NewManager(empty secret) = nil, want error")
	}
	if _, err := NewManager(nil, "short", false, slog.Default()); err == nil {
		t.Fatal("NewManager(short secret) = nil, want error")
	}

	key, err := deriveKey("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	m := &Manager{encKey: key}
	want := &TwitchTokens{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
	}
	encrypted, err := m.encryptTokens(want)
	if err != nil {
		t.Fatalf("encryptTokens: %v", err)
	}
	got, err := m.DecryptTokens(&repository.Session{EncryptedTokens: encrypted})
	if err != nil {
		t.Fatalf("DecryptTokens: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("tokens = %+v, want %+v", got, want)
	}
}
