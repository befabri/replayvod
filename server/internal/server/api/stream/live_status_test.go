package stream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

func TestIsLiveChecksOnlyTheRequestedBroadcaster(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		live bool
	}{
		{"live without follows or local history", `{"data":[{"id":"s1","user_id":"123"}],"pagination":{}}`, true},
		{"offline", `{"data":[],"pagination":{}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := twitch.NewClient("client", "secret", testClientLogger())
			calls := 0
			client.SetHTTPClient(&http.Client{Transport: providerRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/helix/streams" || r.URL.Query().Get("user_id") != "123" || r.URL.Query().Get("first") != "1" {
					t.Fatalf("unexpected live check: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			// A nil repository makes any accidental dependency on local stream or
			// channel rows fail; the transport rejects a follows request as well.
			h := NewHandler(New(nil, client, testClientLogger()), testClientLogger())
			got, err := h.IsLive(twitch.WithUserToken(t.Context(), "test-token"), IsLiveInput{BroadcasterID: "123"})
			if err != nil || got != tc.live || calls != 1 {
				t.Fatalf("IsLive = %v, %v (%d calls), want %v with one call", got, err, calls, tc.live)
			}
		})
	}
}

type unavailableStreamSource struct{ fakeFollowedStreamsSource }

func (unavailableStreamSource) GetStreams(ctx context.Context, input *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	return nil, twitch.Pagination{}, errors.New("Twitch unavailable")
}

func TestIsLiveDoesNotReportUpstreamFailureAsOffline(t *testing.T) {
	h := NewHandler(New(nil, &unavailableStreamSource{}, testClientLogger()), testClientLogger())
	_, err := h.IsLive(t.Context(), IsLiveInput{BroadcasterID: "123"})
	var rpc *trpcgo.Error
	if !errors.As(err, &rpc) || rpc.Code != trpcgo.CodeInternalServerError {
		t.Fatalf("IsLive error = %v, want INTERNAL_SERVER_ERROR", err)
	}
}
