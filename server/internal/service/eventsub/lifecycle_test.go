package eventsub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func TestCreateChannelSubsPanicCancelsAndJoinsSiblingBeforeSettlement(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	for _, id := range []string{"held", "broken"} {
		seedSubscriptionChannel(t, t.Context(), repo, id)
	}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	log := slog.New(slog.DiscardHandler)
	tc := twitch.NewClient("client", "secret", log)
	tc.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Host == "id.twitch.tv" {
			return textResponse(http.StatusOK, `{"access_token":"token","expires_in":3600,"token_type":"bearer"}`), nil
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(body), `"held"`) {
			close(started)
			<-req.Context().Done()
			close(cancelled)
			<-release
			return nil, req.Context().Err()
		}
		<-started
		panic("subscription child failed")
	})})
	svc := New(repo, tc, "https://example.com/callback", "secret", log)
	work := background.New(nil)
	settled := make(chan error, 1)
	if err := work.Start("task", "subscriptions", func(ctx context.Context) error {
		_, err := svc.createChannelSubs(ctx, []createReq{
			{subType: "stream.online", broadcasterID: "held"},
			{subType: "stream.online", broadcasterID: "broken"},
		})
		return err
	}, func(err error) { settled <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("child panic did not cancel sibling")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := work.WaitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subscription task released ownership before sibling returned: %v", err)
	}
	unblock.Do(func() { close(release) })
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := work.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-settled; err == nil || !strings.Contains(err.Error(), "worker panic: subscription child failed") {
		t.Fatalf("child failure did not reach task settlement: %v", err)
	}
	work.Stop()
}
