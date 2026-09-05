package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	scheduleapi "github.com/befabri/replayvod/server/internal/server/api/schedule"
)

func permissionJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestScheduleRequestValidationOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "channel", BroadcasterLogin: "channel", BroadcasterName: "Channel"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, fmt.Sprintf(`{"broadcaster_id":"channel","note":%q}`, strings.Repeat("é", 201))} {
		permissionResponse(t, h, http.MethodPost, "/trpc/schedule.createRequest", body, h.viewer, http.StatusBadRequest)
	}
	if rows, err := h.repo.ListScheduleRequests(ctx, 50, nil); err != nil || len(rows) != 0 {
		t.Fatalf("invalid filing left requests: %+v, %v", rows, err)
	}
	note := strings.Repeat("é", 200)
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.createRequest", permissionJSON(t, map[string]any{"broadcaster_id": "channel", "note": note}), h.viewer, http.StatusOK)
	rows := decodePermissionData[scheduleapi.RequestPageResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/schedule.myRequests", "", h.viewer, http.StatusOK)).Items
	if len(rows) != 1 || rows[0].Note == nil || *rows[0].Note != note || rows[0].RequestedBy != "perm-viewer-1" {
		t.Fatalf("created request = %+v", rows)
	}
	reqID := rows[0].ID
	for _, tc := range []struct {
		name  string
		patch map[string]any
	}{
		{"missing request id", map[string]any{"request_id": nil}},
		{"missing quality", map[string]any{"quality": nil}},
		{"unknown quality", map[string]any{"quality": "ULTRA"}},
		{"unknown recording type", map[string]any{"recording_type": "other"}},
		{"missing minimum viewers", map[string]any{"has_min_viewers": true}},
		{"negative minimum viewers", map[string]any{"has_min_viewers": true, "min_viewers": -1}},
		{"empty categories", map[string]any{"has_categories": true}},
		{"empty tags", map[string]any{"has_tags": true}},
		{"missing retention", map[string]any{"is_delete_rediff": true}},
		{"zero retention", map[string]any{"is_delete_rediff": true, "time_before_delete": 0}},
		{"overflow retention", map[string]any{"is_delete_rediff": true, "time_before_delete": 2562048}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"request_id": reqID, "quality": "HIGH", "category_ids": []string{}, "tag_ids": []int64{}}
			for k, v := range tc.patch {
				if v == nil {
					delete(body, k)
				} else {
					body[k] = v
				}
			}
			permissionResponse(t, h, http.MethodPost, "/trpc/schedule.approveRequest", permissionJSON(t, body), h.admin, http.StatusBadRequest)
			stored, err := h.repo.GetScheduleRequest(ctx, reqID)
			if err != nil || stored.Status != repository.ScheduleRequestStatusPending || stored.DecidedAt != nil || stored.ScheduleID != nil {
				t.Fatalf("invalid approval changed request: %+v, %v", stored, err)
			}
			if schedules, err := h.repo.ListSchedules(ctx, 50, 0); err != nil || len(schedules) != 0 {
				t.Fatalf("invalid approval created schedules: %+v, %v", schedules, err)
			}
		})
	}
}

func TestScheduleApprovalSettingsAttributionAndVisibilityOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "channel", BroadcasterLogin: "channel", BroadcasterName: "Channel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.UpsertCategory(ctx, &repository.Category{ID: "game", Name: "Game"}); err != nil {
		t.Fatal(err)
	}
	tag, err := h.repo.UpsertTag(ctx, "English")
	if err != nil {
		t.Fatal(err)
	}
	req, err := h.repo.CreateScheduleRequest(ctx, "channel", "perm-viewer-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"request_id": req.ID, "recording_type": "audio", "quality": "LOW", "force_h264": true,
		"has_min_viewers": true, "min_viewers": 0, "has_categories": true, "has_tags": true,
		"is_delete_rediff": true, "time_before_delete": 2562047, "is_disabled": true,
		"category_ids": []string{"game"}, "tag_ids": []int64{tag.ID},
	}
	sched := decodePermissionData[scheduleapi.ScheduleResponse](t, permissionResponse(t, h, http.MethodPost, "/trpc/schedule.approveRequest", permissionJSON(t, input), h.admin, http.StatusOK))
	if sched.ID == 0 || sched.BroadcasterID != "channel" || sched.RequestedBy != "perm-admin-1" || sched.RequestedFrom == nil || *sched.RequestedFrom != "perm-viewer-1" || sched.RequestedFromName != "perm-viewer-1" {
		t.Fatalf("approval lost channel or attribution: %+v", sched)
	}
	if sched.RecordingType != "audio" || sched.Quality != "LOW" || sched.ForceH264 || !sched.HasMinViewers || sched.MinViewers == nil || *sched.MinViewers != 0 || !sched.HasCategories || !sched.HasTags || !sched.IsDeleteRediff || sched.TimeBeforeDelete == nil || *sched.TimeBeforeDelete != 2562047 || !sched.IsDisabled {
		t.Fatalf("approval lost edited settings or audio normalization: %+v", sched)
	}
	if !reflect.DeepEqual(sched.Categories, []scheduleapi.CategoryLink{{ID: "game", Name: "Game"}}) || !reflect.DeepEqual(sched.Tags, []scheduleapi.TagLink{{ID: tag.ID, Name: "English"}}) {
		t.Fatalf("approval lost filters: %+v", sched)
	}
	for _, cookie := range []*http.Cookie{h.viewer, h.admin, h.owner} {
		list := decodePermissionData[scheduleapi.ListResponse](t, permissionResponse(t, h, http.MethodGet, queryWithInput("/trpc/schedule.list", `{"limit":50}`), "", cookie, http.StatusOK))
		if len(list.Data) != 1 || !reflect.DeepEqual(list.Data[0], sched) {
			t.Fatalf("global list lost persisted approval data: %+v, want %+v", list, sched)
		}
	}
	for _, tc := range []struct {
		cookie *http.Cookie
		count  int
	}{{h.viewer, 0}, {h.admin, 1}, {h.owner, 0}} {
		mine := decodePermissionData[scheduleapi.ListResponse](t, permissionResponse(t, h, http.MethodGet, queryWithInput("/trpc/schedule.mine", `{"limit":50}`), "", tc.cookie, http.StatusOK))
		if len(mine.Data) != tc.count {
			t.Fatalf("mine count = %d, want %d", len(mine.Data), tc.count)
		}
	}
	for _, proc := range []string{"update", "toggle", "delete"} {
		body := map[string]any{"id": sched.ID}
		if proc == "update" {
			body["quality"] = "HIGH"
		}
		permissionResponse(t, h, http.MethodPost, "/trpc/schedule."+proc, permissionJSON(t, body), h.viewer, http.StatusForbidden)
	}
	delete(input, "request_id")
	input["id"] = sched.ID
	input["quality"] = "MEDIUM"
	updated := decodePermissionData[scheduleapi.ScheduleResponse](t, permissionResponse(t, h, http.MethodPost, "/trpc/schedule.update", permissionJSON(t, input), h.owner, http.StatusOK))
	if updated.RequestedBy != sched.RequestedBy || updated.RequestedFrom == nil || *updated.RequestedFrom != *sched.RequestedFrom || updated.RequestedFromName != sched.RequestedFromName || updated.Quality != "MEDIUM" {
		t.Fatalf("editing changed request attribution: %+v", updated)
	}
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.delete", fmt.Sprintf(`{"id":%d}`, sched.ID), h.admin, http.StatusOK)
	history := decodePermissionData[scheduleapi.RequestPageResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/schedule.myRequests", "", h.viewer, http.StatusOK)).Items
	if len(history) != 1 || history[0].Status != scheduleapi.ScheduleRequestStatusApproved || history[0].ScheduleID != nil || history[0].DecidedBy == nil || *history[0].DecidedBy != "perm-admin-1" || history[0].DecidedAt == nil {
		t.Fatalf("schedule deletion lost approval history: %+v", history)
	}
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.cancelRequest", fmt.Sprintf(`{"id":%d}`, req.ID), h.viewer, http.StatusNotFound)
}

func TestScheduleRequestListsAreScopedOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "channel", BroadcasterLogin: "channel", BroadcasterName: "Channel"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cookie *http.Cookie
		note   string
	}{{h.viewer, "viewer note"}, {h.admin, "admin note"}} {
		permissionResponse(t, h, http.MethodPost, "/trpc/schedule.createRequest", permissionJSON(t, map[string]string{"broadcaster_id": "channel", "note": tc.note}), tc.cookie, http.StatusOK)
	}
	all := decodePermissionData[scheduleapi.RequestPageResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/schedule.requests", "", h.admin, http.StatusOK)).Items
	if len(all) != 2 {
		t.Fatalf("admin queue = %+v, want both users' requests", all)
	}
	for _, tc := range []struct {
		cookie *http.Cookie
		user   string
		note   string
	}{{h.viewer, "perm-viewer-1", "viewer note"}, {h.admin, "perm-admin-1", "admin note"}} {
		mine := decodePermissionData[scheduleapi.RequestPageResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/schedule.myRequests", "", tc.cookie, http.StatusOK)).Items
		if len(mine) != 1 || mine[0].RequestedBy != tc.user || mine[0].Note == nil || *mine[0].Note != tc.note || mine[0].BroadcasterName != "Channel" || mine[0].RequestedByName != tc.user {
			t.Fatalf("myRequests for %s leaked or lost data: %+v", tc.user, mine)
		}
	}
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.rejectRequest", fmt.Sprintf(`{"id":%d}`, all[0].ID), h.owner, http.StatusOK)
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.rejectRequest", fmt.Sprintf(`{"id":%d}`, all[0].ID), h.admin, http.StatusBadRequest)
	permissionResponse(t, h, http.MethodPost, "/trpc/schedule.rejectRequest", `{"id":99999}`, h.admin, http.StatusNotFound)
}

func TestScheduleRequestPaginationOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("page-channel-%d", i)
		if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.CreateScheduleRequest(ctx, id, "perm-viewer-1", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Decode the response independently so handler type changes cannot mask
	// wire incompatibilities.
	type page struct {
		Items      []scheduleapi.ScheduleRequestResponse `json:"items"`
		NextCursor json.RawMessage                       `json:"next_cursor"`
	}
	first := decodePermissionData[page](t, permissionResponse(t, h, http.MethodGet, queryWithInput("/trpc/schedule.myRequests", `{"limit":2}`), "", h.viewer, http.StatusOK))
	if len(first.Items) != 2 || len(first.NextCursor) == 0 {
		t.Fatalf("first page not bounded: %+v", first)
	}
	nextInput := permissionJSON(t, map[string]any{"limit": 2, "cursor": first.NextCursor})
	next := decodePermissionData[page](t, permissionResponse(t, h, http.MethodGet, queryWithInput("/trpc/schedule.myRequests", nextInput), "", h.viewer, http.StatusOK))
	if len(next.Items) != 1 || len(next.NextCursor) != 0 || next.Items[0].ID >= first.Items[1].ID {
		t.Fatalf("next page duplicated or lost history: %+v", next)
	}
	for _, input := range []string{`{"limit":201}`, `{"limit":-1}`, `{"cursor":{"id":0,"created_at":"2026-01-01T00:00:00Z"}}`, `{"cursor":{"id":1}}`} {
		permissionResponse(t, h, http.MethodGet, queryWithInput("/trpc/schedule.myRequests", input), "", h.viewer, http.StatusBadRequest)
	}
}

func TestSharedScheduleSettingsPreserveOptionalUpdateOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	if _, err := h.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "codec-channel", BroadcasterLogin: "codec-channel", BroadcasterName: "Codec"}); err != nil {
		t.Fatal(err)
	}
	created := decodePermissionData[scheduleapi.ScheduleResponse](t, permissionResponse(t, h, http.MethodPost, "/trpc/schedule.create", `{"broadcaster_id":"codec-channel","quality":"HIGH","force_h264":true}`, h.admin, http.StatusOK))
	if !created.ForceH264 {
		t.Fatal("create lost explicit force_h264")
	}
	for _, tc := range []struct {
		setting string
		want    bool
	}{
		{``, true}, {`,"force_h264":null`, true}, {`,"force_h264":false`, false}, {`,"force_h264":true`, true},
	} {
		body := fmt.Sprintf(`{"id":%d,"quality":"HIGH"%s}`, created.ID, tc.setting)
		updated := decodePermissionData[scheduleapi.ScheduleResponse](t, permissionResponse(t, h, http.MethodPost, "/trpc/schedule.update", body, h.admin, http.StatusOK))
		if updated.ForceH264 != tc.want {
			t.Errorf("setting %s: force_h264=%v, want %v", tc.setting, updated.ForceH264, tc.want)
		}
	}
}
