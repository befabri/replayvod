package videodownload

import (
	"net/url"
	"testing"
	"time"
)

const testSecret = "server-hmac-secret"

// signedParams mints a URL and pulls its exp+sig query values back out, so a
// test can feed them to Verify the way the serving route does.
func signedParams(t *testing.T, s *Signer, videoID int64, part int32) (string, string) {
	t.Helper()
	u, err := url.Parse(s.PartURL(videoID, part))
	if err != nil {
		t.Fatalf("mint URL: %v", err)
	}
	q := u.Query()
	return q.Get(ParamExpires), q.Get(ParamSignature)
}

func TestSignVerify_roundTrip(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", time.Hour)
	v := NewVerifier(testSecret)

	exp, sig := signedParams(t, s, 42, 3)
	if err := v.Verify(42, 3, exp, sig); err != nil {
		t.Fatalf("freshly signed URL should verify: %v", err)
	}
}

func TestVerify_rejectsTamperedVideoOrPart(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", time.Hour)
	v := NewVerifier(testSecret)
	exp, sig := signedParams(t, s, 42, 0)

	if err := v.Verify(43, 0, exp, sig); err == nil {
		t.Fatal("a different video id must not verify")
	}
	if err := v.Verify(42, 1, exp, sig); err == nil {
		t.Fatal("a different part index must not verify")
	}
}

func TestVerify_rejectsExpired(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", time.Hour)
	// Freeze the signer's clock in the past so the minted URL is already expired.
	s.now = func() time.Time { return time.Unix(1_000, 0) }
	exp, sig := signedParams(t, s, 42, 0)

	v := NewVerifier(testSecret)
	v.now = func() time.Time { return time.Unix(1_000_000, 0) }
	if err := v.Verify(42, 0, exp, sig); err == nil {
		t.Fatal("an expired URL must not verify")
	}
}

func TestVerify_rejectsWrongSecret(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", time.Hour)
	exp, sig := signedParams(t, s, 42, 0)

	if err := NewVerifier("different-secret").Verify(42, 0, exp, sig); err == nil {
		t.Fatal("a verifier with a different secret must not verify")
	}
}

func TestVerify_rejectsGarbageExpiry(t *testing.T) {
	v := NewVerifier(testSecret)
	if err := v.Verify(42, 0, "not-a-number", "deadbeef"); err == nil {
		t.Fatal("a non-numeric expiry must not verify")
	}
}

func TestSigner_disabledWhenNoBaseOrTTL(t *testing.T) {
	if NewSigner(testSecret, "", time.Hour).Enabled() {
		t.Fatal("empty base URL should disable the signer")
	}
	if NewSigner(testSecret, "https://app.example", 0).Enabled() {
		t.Fatal("zero TTL should disable the signer")
	}
	if got := NewSigner(testSecret, "", time.Hour).PartURL(1, 0); got != "" {
		t.Fatalf("disabled signer should mint no URL, got %q", got)
	}
}

func TestPartURL_shape(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example/", time.Hour) // trailing slash trimmed
	u, err := url.Parse(s.PartURL(7, 2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Scheme != "https" || u.Host != "app.example" {
		t.Fatalf("origin wrong: %q", u.String())
	}
	if u.Path != "/api/v1/videos/7/parts/2/download" {
		t.Fatalf("path = %q", u.Path)
	}
}

func TestPartURLUntil_capsExpiry(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", 24*time.Hour)
	now := time.Unix(1_000, 0)
	s.now = func() time.Time { return now }
	capAt := now.Add(time.Hour)

	u, err := url.Parse(s.PartURLUntil(7, 2, &capAt))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := u.Query().Get(ParamExpires); got != "4600" {
		t.Fatalf("exp = %q, want capped deadline 4600", got)
	}
}

func TestPartURLUntil_omitsWhenCapReached(t *testing.T) {
	s := NewSigner(testSecret, "https://app.example", time.Hour)
	now := time.Unix(1_000, 0)
	s.now = func() time.Time { return now }
	capAt := now

	if got := s.PartURLUntil(7, 2, &capAt); got != "" {
		t.Fatalf("PartURLUntil with reached cap = %q, want empty", got)
	}
}

// TestDeriveKey_domainSeparated guards the security property that the download
// signing key is NOT the raw HMAC secret: a signature minted here must not equal
// one computed directly under the server secret over the same logical message,
// so a download signature can never be replayed against the EventSub verifier.
func TestDeriveKey_domainSeparated(t *testing.T) {
	derived := deriveKey(testSecret)
	if string(derived) == testSecret {
		t.Fatal("derived key must not equal the raw secret")
	}
	// Same input bytes, different keys → different signatures.
	a := computeSig(derived, 1, 0, 1234)
	b := computeSig([]byte(testSecret), 1, 0, 1234)
	if a == b {
		t.Fatal("download signature must differ from one under the raw secret")
	}
}
