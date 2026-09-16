package contracttest

import (
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testVideoColumnsRoundTrip pins every video field the list query exposes.
// A column added to the videos table needs a field on repository.Video and a
// check here.
func testVideoColumnsRoundTrip(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-rt")
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: "stream-rt", BroadcasterID: "bc-rt", Type: "live", Language: "en", ViewerCount: 1, StartedAt: time.Now().UTC().Truncate(time.Second)}); err != nil {
		t.Fatal(err)
	}
	streamID, window := "stream-rt", int64(72)
	in := &repository.VideoInput{
		JobID: "job-rt", Filename: "filename-rt", DisplayName: "Display Name RT",
		Status: repository.VideoStatusDone, Quality: repository.QualityHigh, BroadcasterID: "bc-rt", StreamID: &streamID,
		ViewerCount: 1234, Language: "en", RecordingType: repository.RecordingTypeVideo, ForceH264: true, RetentionWindowHours: &window,
	}
	v, err := repo.CreateVideo(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	fps := 60.0
	if err := repo.UpdateVideoSelectedVariant(ctx, v.ID, "1080", &fps); err != nil {
		t.Fatal(err)
	}
	thumb := "thumbnails/thumb.jpg"
	if err := repo.MarkVideoDone(ctx, v.ID, 3600.5, 1<<30, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListVideos(ctx, repository.ListVideosOpts{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %+v, %v", rows, err)
	}
	got := rows[0]
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"ID", got.ID, v.ID},
		{"JobID", got.JobID, in.JobID},
		{"Filename", got.Filename, in.Filename},
		{"DisplayName", got.DisplayName, in.DisplayName},
		{"Status", got.Status, repository.VideoStatusDone},
		{"Quality", got.Quality, in.Quality},
		{"SelectedQuality", derefString(got.SelectedQuality), "1080"},
		{"SelectedFPS", derefFloat64(got.SelectedFPS), fps},
		{"BroadcasterID", got.BroadcasterID, in.BroadcasterID},
		{"StreamID", derefString(got.StreamID), streamID},
		{"ViewerCount", got.ViewerCount, in.ViewerCount},
		{"Language", got.Language, in.Language},
		{"DurationSeconds", derefFloat64(got.DurationSeconds), 3600.5},
		{"SizeBytes", derefInt64(got.SizeBytes), int64(1 << 30)},
		{"Thumbnail", derefString(got.Thumbnail), thumb},
		{"RecordingType", got.RecordingType, in.RecordingType},
		{"ForceH264", got.ForceH264, in.ForceH264},
		{"RetentionWindowHours", derefInt64(got.RetentionWindowHours), window},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if got.StartDownloadAt.IsZero() || got.DownloadedAt == nil || got.DownloadedAt.IsZero() {
		t.Errorf("timestamps not stamped: start %v, downloaded %v", got.StartDownloadAt, got.DownloadedAt)
	}
}

// testVideoRetentionRefsOutliveSchedule pins that a recording keeps the
// retention window captured at creation after the schedule that supplied
// it is deleted, while the references to that schedule are cleared.
func testVideoRetentionRefsOutliveSchedule(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")
	hours := int64(24)
	sched, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-1", Quality: repository.QualityHigh, IsDeleteRediff: true, TimeBeforeDelete: &hours})
	if err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec-1", DisplayName: "b-1", Status: repository.VideoStatusPending,
		Quality: repository.QualityHigh, BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		TriggerScheduleID: &sched.ID, RetentionSourceScheduleID: &sched.ID, RetentionWindowHours: &hours,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || derefInt64(got.TriggerScheduleID) != sched.ID || derefInt64(got.RetentionSourceScheduleID) != sched.ID || derefInt64(got.RetentionWindowHours) != hours {
		t.Fatalf("video with schedule refs = %+v, %v", got, err)
	}
	if err := repo.DeleteSchedule(ctx, sched.ID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetVideo(ctx, v.ID)
	if err != nil || got.TriggerScheduleID != nil || got.RetentionSourceScheduleID != nil || derefInt64(got.RetentionWindowHours) != hours {
		t.Fatalf("video after schedule delete = %+v, %v", got, err)
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListRetentionCandidates(ctx, time.Now().UTC().Add(48*time.Hour), 0, 100)
	if err != nil || len(rows) != 1 || rows[0].VideoID != v.ID {
		t.Fatalf("candidates after schedule delete = %+v, %v; the captured window must still govern", rows, err)
	}
}
