package main

import "testing"

func TestValidateRelayURLs_RejectsTokenMismatch(t *testing.T) {
	const (
		ingestURL    = "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA"
		subscribeURL = "wss://relay.replayvod.com/u/BBBBBBBBBBBBBBBB/subscribe"
	)

	err := validateRelayURLs(ingestURL, subscribeURL)
	if err == nil {
		t.Fatal("validateRelayURLs(token mismatch) = nil, want error")
	}
}

func TestValidateRelayURLs_RejectsHostMismatch(t *testing.T) {
	err := validateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://other.example/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(host mismatch) = nil, want error")
	}
}

func TestValidateRelayURLs_RejectsPlaintextSubscribeURL(t *testing.T) {
	err := validateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"ws://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(ws subscribe) = nil, want error")
	}
}

// TestValidateRelayURLs_AcceptsAlignedConfig pins the existing behavior:
// when RELAY_SUBSCRIBE_URL is set and WEBHOOK_CALLBACK_URL is a usable HTTPS
// endpoint, validation must succeed.
func TestValidateRelayURLs_AcceptsAlignedConfig(t *testing.T) {
	err := validateRelayURLs(
		"https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err != nil {
		t.Fatalf("validateRelayURLs(aligned) = %v, want nil", err)
	}
}

// TestValidateRelayURLs_RejectsUnusableWebhookCallback pins the existing
// behavior that a non-HTTPS callback fails when RELAY_SUBSCRIBE_URL is set.
func TestValidateRelayURLs_RejectsUnusableWebhookCallback(t *testing.T) {
	err := validateRelayURLs(
		"http://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		"wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
	)
	if err == nil {
		t.Fatal("validateRelayURLs(unusable callback) = nil, want error")
	}
}

// TestValidateRelayURLs_RelayDisabled pins the existing behavior that an
// empty RELAY_SUBSCRIBE_URL disables the check entirely.
func TestValidateRelayURLs_RelayDisabled(t *testing.T) {
	err := validateRelayURLs("", "")
	if err != nil {
		t.Fatalf("validateRelayURLs(empty) = %v, want nil", err)
	}
}
