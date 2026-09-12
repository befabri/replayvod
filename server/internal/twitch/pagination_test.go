package twitch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCheckPageCursor(t *testing.T) {
	cases := []struct {
		name           string
		previous, next string
		page, max      int
		wantStalled    bool
	}{
		{name: "advances", previous: "a", next: "b", page: 1, max: 10},
		{name: "first page after empty cursor", previous: "", next: "a", page: 1, max: 10},
		{name: "repeated cursor", previous: "a", next: "a", page: 1, max: 10, wantStalled: true},
		{name: "last allowed page", previous: "a", next: "b", page: 9, max: 10},
		{name: "page cap", previous: "a", next: "b", page: 10, max: 10, wantStalled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPageCursor(tc.previous, tc.next, tc.page, tc.max)
			if errors.Is(err, ErrPaginationStalled) != tc.wantStalled {
				t.Fatalf("CheckPageCursor(%q, %q, %d, %d) = %v, want stalled=%v", tc.previous, tc.next, tc.page, tc.max, err, tc.wantStalled)
			}
		})
	}
}

// pagingClient answers every Helix request with one stream and the cursor
// chosen by cursorFor, counting calls.
func pagingClient(calls *atomic.Int32, cursorFor func(call int32) string) *Client {
	client := NewClient("client-id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		call := calls.Add(1)
		body := fmt.Sprintf(`{"data":[{"id":"%d","user_id":"u1"}],"pagination":{"cursor":%q}}`, call, cursorFor(call))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	return client
}

// TestGetStreamsAllStopsOnStalledPagination pins the generated drain helpers:
// an echoed cursor and an endless cursor sequence both end with
// ErrPaginationStalled instead of looping, and a cancelled context ends the
// drain before the next request.
func TestGetStreamsAllStopsOnStalledPagination(t *testing.T) {
	ctx := WithUserToken(context.Background(), "tok")

	t.Run("repeated cursor", func(t *testing.T) {
		var calls atomic.Int32
		client := pagingClient(&calls, func(int32) string { return "same" })
		_, _, err := client.GetStreamsAll(ctx, &GetStreamsParams{UserID: []string{"u1"}})
		if !errors.Is(err, ErrPaginationStalled) {
			t.Fatalf("GetStreamsAll = %v, want ErrPaginationStalled", err)
		}
		if got := calls.Load(); got != 2 {
			t.Fatalf("requests = %d, want 2", got)
		}
	})

	t.Run("page cap", func(t *testing.T) {
		var calls atomic.Int32
		client := pagingClient(&calls, func(call int32) string { return fmt.Sprint(call) })
		_, _, err := client.GetStreamsAll(ctx, &GetStreamsParams{UserID: []string{"u1"}})
		if !errors.Is(err, ErrPaginationStalled) {
			t.Fatalf("GetStreamsAll = %v, want ErrPaginationStalled", err)
		}
		if got := calls.Load(); got != maxPaginationPages {
			t.Fatalf("requests = %d, want %d", got, maxPaginationPages)
		}
	})

	t.Run("cancelled between pages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var calls atomic.Int32
		client := pagingClient(&calls, func(call int32) string {
			cancel()
			return fmt.Sprint(call)
		})
		_, _, err := client.GetStreamsAll(ctx, &GetStreamsParams{UserID: []string{"u1"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetStreamsAll = %v, want context.Canceled", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("requests = %d, want 1", got)
		}
	})
}

func TestGetStreamsAllAccumulatesSuccessfulPages(t *testing.T) {
	var calls atomic.Int32
	client := pagingClient(&calls, func(call int32) string {
		if call == 3 {
			return ""
		}
		return fmt.Sprint(call)
	})
	streams, _, err := client.GetStreamsAll(WithUserToken(t.Context(), "tok"), &GetStreamsParams{UserID: []string{"u1"}})
	if err != nil || len(streams) != 3 || calls.Load() != 3 {
		t.Fatalf("drain: %v, %v; calls=%d", streams, err, calls.Load())
	}
	for i, stream := range streams {
		if stream.ID != fmt.Sprint(i+1) {
			t.Fatalf("page %d: stream=%+v", i+1, stream)
		}
	}
}
