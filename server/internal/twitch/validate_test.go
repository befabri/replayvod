package twitch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/validate"
	"github.com/go-playground/validator/v10"
)

// failingRoundTripper fails the enclosing test if any HTTP round-trip happens.
// Used by pre-flight validation tests to assert the wire is never hit.
type failingRoundTripper struct{ t *testing.T }

func (f failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Fatal("request hit the wire; pre-flight validation should have blocked it")
	return nil, errors.New("unreachable")
}

func newNoNetworkClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient("test-client-id", "test-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.httpClient = &http.Client{Transport: failingRoundTripper{t}}
	// Skip the app-token acquisition path too — otherwise do() would call out
	// to id.twitch.tv before validation runs on the first request. Pre-seeding
	// the cached token keeps `appAccessToken` from touching the network.
	c.appToken = "seeded-for-tests"
	c.appTokenExp = time.Now().Add(1 * time.Hour)
	return c
}

func TestClient_InvalidParams_FailsBeforeWire(t *testing.T) {
	c := newNoNetworkClient(t)

	// GetGamesParams.ID is `max=100`. 101 entries must fail validation.
	params := &GetGamesParams{ID: make([]string, 101)}
	for i := range params.ID {
		params.ID[i] = "x"
	}

	err := c.get(context.Background(), "/games", params, nil)
	if err == nil {
		t.Fatal("expected validation error; got nil")
	}
	var vErrs validator.ValidationErrors
	if !errors.As(err, &vErrs) {
		t.Fatalf("expected wrapped validator.ValidationErrors; got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "ID") {
		t.Errorf("error should name the offending field ID; got %q", err.Error())
	}
}

func TestClient_InvalidBody_FailsBeforeWire(t *testing.T) {
	c := newNoNetworkClient(t)

	// ModifyChannelInformationBody.Tags is `max=10,dive,max=25`. 11 entries
	// violates the array max.
	body := &ModifyChannelInformationBody{Tags: make([]string, 11)}

	err := c.patch(context.Background(), "/channels", nil, body, nil)
	if err == nil {
		t.Fatal("expected validation error; got nil")
	}
	var vErrs validator.ValidationErrors
	if !errors.As(err, &vErrs) {
		t.Fatalf("expected wrapped validator.ValidationErrors; got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Tags") {
		t.Errorf("error should name the offending field; got %q", err.Error())
	}
}

// TestGeneratedTags_AreSyntacticallyValid proves every generated validate tag
// parses without error — a failure here means the template emitted a malformed
// tag (e.g. unescaped enum value or a missing operand). Calls validate.V.Struct
// on zero values of each request-side type; invalid tags surface as a non-
// ValidationErrors error. Valid-but-unsatisfied validation (required failing
// on a zero struct) is expected for required-field types and is ignored.
func TestGeneratedTags_AreSyntacticallyValid(t *testing.T) {
	// Zero value of every request-side type we generate. Add entries when the
	// endpoint filter grows.
	cases := []any{
		&GetUsersParams{},
		&GetChannelInformationParams{},
		&ModifyChannelInformationBody{},
		&GetFollowedChannelsParams{},
		&GetGamesParams{},
		&GetTopGamesParams{},
		&GetStreamsParams{},
		&GetFollowedStreamsParams{},
		&GetVideosParams{},
		&CreateEventSubSubscriptionBody{},
		&DeleteEventSubSubscriptionParams{},
		&GetEventSubSubscriptionsParams{},
	}
	for _, tc := range cases {
		err := validate.V.Struct(tc)
		if err == nil {
			continue // zero value happened to satisfy
		}
		// validator.ValidationErrors is expected when a zero-value field trips
		// a constraint it was supposed to; anything else means our generated
		// tag was malformed.
		var vErrs validator.ValidationErrors
		if !errors.As(err, &vErrs) {
			t.Errorf("Struct(%T) returned non-validation error (likely malformed tag): %v", tc, err)
		}
	}
}
