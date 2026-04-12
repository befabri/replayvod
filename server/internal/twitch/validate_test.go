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

// okRoundTripper returns an empty-body 200 for every request. Used by tests
// that want to exercise the post-validation path (e.g. fetch log recorder)
// without actually talking to Twitch.
type okRoundTripper struct{}

func (okRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
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

// newRecordingClient returns a client whose HTTP layer always answers 200 with
// an empty helix envelope. Pairs with a fetch log recorder for round-trip tests.
func newRecordingClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient("test-client-id", "test-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.httpClient = &http.Client{Transport: okRoundTripper{}}
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

// TestIsEnabledFalse_PassesValidation regression-tests Bug A: validator's
// `required` on a bool rejects the zero value `false`, but `is_enabled: false`
// is a legitimate Twitch request (disables a content-classification label).
// The generator drops `required` for bool fields so this body validates.
// Would fail if composeValidateTag re-emits `required` on bools.
func TestIsEnabledFalse_PassesValidation(t *testing.T) {
	body := &ModifyChannelInformationBody{
		ContentClassificationLabels: []ModifyChannelInformationBodyContentClassificationLabel{
			{ID: "DebatedSocialIssuesAndPolitics", IsEnabled: false},
		},
	}
	if err := validate.V.Struct(body); err != nil {
		t.Fatalf("is_enabled=false should pass validation; got %v", err)
	}
}

// TestClient_MissingCondition_FailsBeforeWire regression-tests Bug B: required
// Condition / Transport fields on CreateEventSubSubscriptionBody previously
// had no validate tag because the interface-field override in toStructFieldModel
// bypassed ValidateTag composition. A nil Condition should now fail pre-flight.
func TestClient_MissingCondition_FailsBeforeWire(t *testing.T) {
	c := newNoNetworkClient(t)
	body := &CreateEventSubSubscriptionBody{
		Type:      "stream.online",
		Version:   "1",
		Condition: nil, // required but absent
		Transport: WebhookTransport{Method: "webhook", Callback: "https://example.com"},
	}
	err := c.post(context.Background(), "/eventsub/subscriptions", nil, body, nil)
	if err == nil {
		t.Fatal("expected validation error for missing Condition; got nil")
	}
	var vErrs validator.ValidationErrors
	if !errors.As(err, &vErrs) {
		t.Fatalf("expected wrapped validator.ValidationErrors; got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Condition") {
		t.Errorf("error should name Condition; got %q", err.Error())
	}
}

// fakeFetchLogRecorder captures FetchLogEntry values for assertions. Used by
// the fetch-log regression tests; not thread-safe (tests are serial).
type fakeFetchLogRecorder struct{ entries []FetchLogEntry }

func (f *fakeFetchLogRecorder) RecordFetch(_ context.Context, e FetchLogEntry) {
	f.entries = append(f.entries, e)
}

// TestRecord_BroadcasterIDFromSliceParams regression-tests that record()
// populates FetchLogEntry.BroadcasterID from params.BroadcasterID when the
// generator emitted it as a []string slice. Previous versions promised
// extraction in a comment but never implemented it — the audit column stayed
// null, breaking the system.fetchLogs UI.
func TestRecord_BroadcasterIDFromSliceParams(t *testing.T) {
	rec := &fakeFetchLogRecorder{}
	c := newRecordingClient(t)
	c.SetFetchLogRecorder(rec)

	// GetChannelInformation uses []string for BroadcasterID.
	params := &GetChannelInformationParams{BroadcasterID: []string{"12345", "67890"}}
	_ = c.get(context.Background(), "/channels", params, new(helixResponse[ChannelInformation]))

	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 fetch log entry; got %d", len(rec.entries))
	}
	got := rec.entries[0].BroadcasterID
	if got == nil {
		t.Fatal("BroadcasterID not set")
	}
	if *got != "12345" {
		t.Errorf("BroadcasterID = %q; want %q (first of slice)", *got, "12345")
	}
}

// TestRecord_BroadcasterIDFromScalarParams covers the scalar field shape
// (e.g. ModifyChannelInformationParams.BroadcasterID string).
func TestRecord_BroadcasterIDFromScalarParams(t *testing.T) {
	rec := &fakeFetchLogRecorder{}
	c := newRecordingClient(t)
	c.SetFetchLogRecorder(rec)

	params := &ModifyChannelInformationParams{BroadcasterID: "42"}
	_ = c.patch(context.Background(), "/channels", params, &ModifyChannelInformationBody{}, nil)

	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 fetch log entry; got %d", len(rec.entries))
	}
	got := rec.entries[0].BroadcasterID
	if got == nil || *got != "42" {
		t.Errorf("BroadcasterID = %v; want *\"42\"", got)
	}
}

// TestRecord_NoBroadcasterIDWhenAbsent — params without a BroadcasterID field
// (e.g. GetUsersParams) produce a nil BroadcasterID in the log entry. Also
// covers nil params entirely.
func TestRecord_NoBroadcasterIDWhenAbsent(t *testing.T) {
	rec := &fakeFetchLogRecorder{}
	c := newRecordingClient(t)
	c.SetFetchLogRecorder(rec)

	_ = c.get(context.Background(), "/users", &GetUsersParams{}, new(helixResponse[User]))
	_ = c.get(context.Background(), "/users", nil, new(helixResponse[User]))

	if len(rec.entries) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(rec.entries))
	}
	for i, e := range rec.entries {
		if e.BroadcasterID != nil {
			t.Errorf("entry %d: BroadcasterID = %v; want nil", i, *e.BroadcasterID)
		}
	}
}

// TestExtractBroadcasterID_Unit covers the helper directly without the HTTP layer.
func TestExtractBroadcasterID_Unit(t *testing.T) {
	cases := []struct {
		name   string
		params any
		want   *string
	}{
		{"nil interface", nil, nil},
		{"typed nil pointer", (*GetChannelInformationParams)(nil), nil},
		{"scalar populated", &ModifyChannelInformationParams{BroadcasterID: "7"}, strPtr("7")},
		{"scalar empty", &ModifyChannelInformationParams{BroadcasterID: ""}, nil},
		{"slice populated", &GetChannelInformationParams{BroadcasterID: []string{"a", "b"}}, strPtr("a")},
		{"slice empty", &GetChannelInformationParams{BroadcasterID: []string{}}, nil},
		{"slice nil", &GetChannelInformationParams{}, nil},
		{"no BroadcasterID field", &GetUsersParams{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractBroadcasterID(c.params)
			switch {
			case got == nil && c.want == nil:
				return
			case got == nil || c.want == nil:
				t.Fatalf("got %v, want %v", got, c.want)
			case *got != *c.want:
				t.Errorf("got %q, want %q", *got, *c.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

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
