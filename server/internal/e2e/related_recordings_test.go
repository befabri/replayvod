//go:build integration

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	videoapi "github.com/befabri/replayvod/server/internal/server/api/video"
)

func TestRelatedRecordingsAcrossDatabaseBackends(t *testing.T) {
	for _, d := range []driver{driverSQLite, driverPG} {
		t.Run(string(d), func(t *testing.T) {
			ts := newTestServer(t, d)
			ctx := t.Context()
			if _, err := ts.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "related", BroadcasterLogin: "related", BroadcasterName: "Related"}); err != nil {
				t.Fatal(err)
			}
			var members []*repository.Video
			for i := 0; i < 15; i++ {
				streamID := fmt.Sprintf("broadcast-%02d", i)
				input := &repository.VideoInput{JobID: fmt.Sprintf("job-%02d", i), Filename: fmt.Sprintf("recording-%02d", i), DisplayName: "Related", BroadcasterID: "related", Status: repository.VideoStatusPending, Quality: repository.QualityHigh, StreamID: &streamID, IntentID: "intent"}
				if i == 0 {
					input.IntentParams = json.RawMessage(`{}`)
					input.RestartWaitSeconds = 120
				} else {
					input.IntentPreviousJobID = members[i-1].JobID
					input.IntentObservedAt = time.Now().UTC()
					if err := ts.repo.SetRecordingIntentWaiting(ctx, "intent", input.IntentPreviousJobID, input.IntentObservedAt.Add(120*time.Second)); err != nil {
						t.Fatal(err)
					}
				}
				v, err := repository.CreateAttempt(ctx, ts.repo, input, json.RawMessage(`{"stage":"AUTH","current_part_index":1}`))
				if err != nil {
					t.Fatal(err)
				}
				members = append(members, v)
			}
			if err := ts.repo.MarkVideoFailed(ctx, members[6].ID, "cancelled", repository.CompletionKindCancelled, false); err != nil {
				t.Fatal(err)
			}
			if err := ts.repo.SoftDeleteVideo(ctx, members[8].ID, repository.DeletionKindMissing); err != nil {
				t.Fatal(err)
			}
			unrelated, err := ts.repo.CreateVideo(ctx, &repository.VideoInput{JobID: "unrelated", Filename: "unrelated", BroadcasterID: "related", DisplayName: "Related", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
			if err != nil {
				t.Fatal(err)
			}
			var out videoapi.RelatedRecordingsResponse
			trpcQuery(t, ts, "video.relatedRecordings", map[string]any{"id": members[7].ID}, &out)
			if len(out.Items) != 11 || out.IntentID != "intent" || out.Items[0].ID != members[2].ID || out.Items[10].ID != members[12].ID {
				t.Fatalf("wrong bounded related window: %+v", out)
			}
			if out.Status != videoapi.RecordingIntentStatusActive || out.Items[4].CompletionKind != videoapi.CompletionKindCancelled || out.Items[4].Status != videoapi.VideoStatusFailed {
				t.Fatalf("intent or cancellation state lost: %+v", out)
			}
			if out.Items[6].DeletedAt == nil {
				t.Fatal("removed successor lost its relationship")
			}
			trpcQuery(t, ts, "video.relatedRecordings", map[string]any{"id": unrelated.ID}, &out)
			if len(out.Items) != 0 {
				t.Fatalf("same-channel recording linked without an intent: %+v", out)
			}
			if status, _ := rawRequest(t, ts, http.MethodGet, "video.relatedRecordings", map[string]any{"id": members[0].ID}, ""); status != http.StatusUnauthorized {
				t.Fatalf("anonymous relationship query=%d", status)
			}
			if _, err := ts.repo.UpsertUser(ctx, &repository.User{ID: "related-viewer", Login: "relatedviewer", DisplayName: "Viewer", Role: "viewer"}); err != nil {
				t.Fatal(err)
			}
			session := seedSession(t, ts.repo, ts.sessionMgr, "related-viewer")
			if status, body := rawRequest(t, ts, http.MethodGet, "video.relatedRecordings", map[string]any{"id": members[8].ID}, session); status != http.StatusOK {
				t.Fatalf("viewer cannot navigate removed member: %d %s", status, body)
			}
		})
	}
}
