package recordingwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

func TestValidateURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"https public", "https://hooks.example.com/recordings", true},
		{"https with port + path", "https://example.com:8443/a/b?c=d", true},
		{"http loopback localhost", "http://localhost:9000/hook", true},
		{"http loopback 127", "http://127.0.0.1:9000/hook", true},
		{"http loopback ipv6", "http://[::1]:9000/hook", true},
		// http to a private/LAN address is allowed: the common homelab case of a
		// receiver (media server, notifier, NAS) on the same network with no TLS.
		{"http private 192.168", "http://192.168.1.50:8096/hook", true},
		{"http private 10.x", "http://10.0.0.5/hook", true},
		{"http private 172.16", "http://172.16.3.4:3000/x", true},
		{"https private allowed", "https://192.168.1.50/hook", true},
		{"http public rejected", "http://hooks.example.com/recordings", false},
		// Link-local / cloud-metadata is rejected for ANY scheme.
		{"https link-local metadata rejected", "https://169.254.169.254/latest/meta-data", false},
		{"http link-local rejected", "http://169.254.169.254/", false},
		{"https scoped ipv6 link-local rejected", "https://[fe80::1%25eth0]/hook", false},
		{"http scoped ipv6 link-local rejected", "http://[fe80::1%25eth0]/hook", false},
		{"ftp rejected", "ftp://example.com/x", false},
		{"no scheme rejected", "example.com/hook", false},
		{"no host rejected", "https://", false},
		{"garbage rejected", "://nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateURL(tc.url)
			if tc.ok && err != nil {
				t.Fatalf("validateURL(%q) = %v, want nil", tc.url, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("validateURL(%q) = nil, want error", tc.url)
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("validateURL(%q) error not ErrInvalid: %v", tc.url, err)
				}
			}
		})
	}
}

func TestNormalizeEvents(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"nil means all", nil, nil, false},
		{"blanks mean all", []string{"", "  "}, nil, false},
		{"single", []string{"recording.failed"}, []string{"recording.failed"}, false},
		{"dedup + canonical order", []string{"recording.failed", "recording.completed", "recording.failed"}, []string{"recording.completed", "recording.failed"}, false},
		{"trims whitespace", []string{" recording.completed "}, []string{"recording.completed"}, false},
		{"unknown rejected", []string{"recording.started"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeEvents(tc.in)
			if tc.wantErr {
				if err == nil || !errors.Is(err, ErrInvalid) {
					t.Fatalf("normalizeEvents(%v) err = %v, want ErrInvalid", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEvents(%v) unexpected err: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeEvents(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseEvents(t *testing.T) {
	if got := parseEvents(""); got != nil {
		t.Fatalf("parseEvents(\"\") = %v, want nil", got)
	}
	if got := parseEvents("  "); got != nil {
		t.Fatalf("parseEvents(blank) = %v, want nil", got)
	}
	got := parseEvents("recording.completed,recording.failed")
	want := []string{"recording.completed", "recording.failed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseEvents = %v, want %v", got, want)
	}
}

func TestEventForKind(t *testing.T) {
	if eventForKind(eventbus.RecordingCompleted) != EventCompleted {
		t.Fatal("completed kind should map to recording.completed")
	}
	if eventForKind(eventbus.RecordingFailed) != EventFailed {
		t.Fatal("failed kind should map to recording.failed")
	}
	if eventForKind("nonsense") != "" {
		t.Fatal("unknown kind should map to empty string")
	}
}

// TestSign pins the outbound signature to the exact inbound EventSub formula so
// a receiver verifies a delivery with the same HMAC-SHA256(id‖timestamp‖body)
// computation it uses for Twitch.
func TestSign(t *testing.T) {
	secret := "shared-secret"
	id := "abc123"
	ts := "2026-05-30T12:00:00Z"
	body := []byte(`{"event":"recording.completed"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte(ts))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if got := sign(secret, id, ts, body); got != want {
		t.Fatalf("sign = %q, want %q", got, want)
	}
}

func TestNewMessageIDUnique(t *testing.T) {
	a, err := newMessageID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newMessageID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("message ids must be unique")
	}
	if len(a) != 32 { // 16 bytes hex
		t.Fatalf("message id length = %d, want 32", len(a))
	}
}
