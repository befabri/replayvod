package video

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type fakeWaveformGenerator struct {
	calls []fakeWaveformCall
}

type fakeWaveformCall struct {
	body            string
	durationSeconds float64
	points          int
}

type waveformFlightTestResult struct {
	resp   AudioWaveformResponse
	status int
	err    error
}

type fsNotExistOnlyError struct {
	path string
}

func (e fsNotExistOnlyError) Error() string {
	return "not found: " + e.path
}

func (fsNotExistOnlyError) Is(target error) bool {
	return target == fs.ErrNotExist
}

func (f *fakeWaveformGenerator) Generate(_ context.Context, inputPath string, durationSeconds float64, points int) ([]float32, error) {
	body, _ := os.ReadFile(inputPath)
	f.calls = append(f.calls, fakeWaveformCall{
		body:            string(body),
		durationSeconds: durationSeconds,
		points:          points,
	})
	peaks := make([]float32, points)
	value := float32(len(f.calls)) / 10
	for i := range peaks {
		peaks[i] = value
	}
	return peaks, nil
}

func getWaveform(t *testing.T, srvURL string, videoID int64) *http.Response {
	t.Helper()
	resp, err := http.Get(srvURL + "/api/v1/videos/" + strconv.FormatInt(videoID, 10) + "/waveform")
	if err != nil {
		t.Fatalf("GET waveform: %v", err)
	}
	return resp
}

func decodeWaveform(t *testing.T, r io.Reader) AudioWaveformResponse {
	t.Helper()
	var out AudioWaveformResponse
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatalf("decode waveform: %v", err)
	}
	return out
}

func TestWaveformFlightsCallerCancellationDoesNotCancelSharedBuild(t *testing.T) {
	flights := newWaveformFlights()
	started := make(chan struct{})
	release := make(chan struct{})
	buildCtxErr := make(chan error, 1)
	firstDone := make(chan waveformFlightTestResult, 1)
	firstCtx, firstCancel := context.WithCancel(context.Background())

	go func() {
		resp, status, err := flights.Do(firstCtx, "fingerprint", func(ctx context.Context) (AudioWaveformResponse, int, error) {
			close(started)
			<-release
			buildCtxErr <- ctx.Err()
			return AudioWaveformResponse{DurationSeconds: 12}, http.StatusOK, nil
		})
		firstDone <- waveformFlightTestResult{resp: resp, status: status, err: err}
	}()

	<-started

	secondDone := make(chan waveformFlightTestResult, 1)
	go func() {
		resp, status, err := flights.Do(context.Background(), "fingerprint", func(context.Context) (AudioWaveformResponse, int, error) {
			return AudioWaveformResponse{}, http.StatusInternalServerError, errors.New("duplicate build")
		})
		secondDone <- waveformFlightTestResult{resp: resp, status: status, err: err}
	}()

	select {
	case second := <-secondDone:
		t.Fatalf("second caller completed before shared build release: status=%d err=%v", second.status, second.err)
	case <-time.After(100 * time.Millisecond):
	}

	firstCancel()

	select {
	case first := <-firstDone:
		if first.status != statusClientClosed {
			t.Fatalf("first status = %d, want %d", first.status, statusClientClosed)
		}
		if !errors.Is(first.err, context.Canceled) {
			t.Fatalf("first err = %v, want context.Canceled", first.err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("canceled caller waited for shared waveform build")
	}

	close(release)

	select {
	case second := <-secondDone:
		if second.err != nil {
			t.Fatalf("second err = %v, want nil", second.err)
		}
		if second.status != http.StatusOK {
			t.Fatalf("second status = %d, want 200", second.status)
		}
		if second.resp.DurationSeconds != 12 {
			t.Fatalf("second duration = %v, want 12", second.resp.DurationSeconds)
		}
	case <-time.After(time.Second):
		t.Fatal("second caller did not receive shared waveform result")
	}

	if err := <-buildCtxErr; err != nil {
		t.Fatalf("shared build context was canceled: %v", err)
	}
}

func TestAudioWaveformServesAudioOnlyPart(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-01.m4a": []byte("audio-part"),
	}}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	body := decodeWaveform(t, resp.Body)
	if body.DurationSeconds != 2 {
		t.Fatalf("duration = %v, want 2", body.DurationSeconds)
	}
	if len(body.Peaks) != waveformMinPoints {
		t.Fatalf("peaks len = %d, want %d", len(body.Peaks), waveformMinPoints)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want 1", len(generator.calls))
	}
	call := generator.calls[0]
	if call.body != "audio-part" || call.durationSeconds != 2 || call.points != waveformMinPoints {
		t.Fatalf("generator call = %+v", call)
	}
	if !containsString(store.opened, "videos/vod-42-01.m4a") {
		t.Fatalf("opened paths = %#v, want video part opened", store.opened)
	}
}

func TestAudioWaveformRejectsVideoRecording(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-01.mp4": []byte("video-part"),
	}}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeVideo
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0", len(generator.calls))
	}
}

func TestAudioWaveformAppendsMultipartAudio(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-01.m4a": []byte("part-one"),
		"videos/vod-42-02.m4a": []byte("part-two"),
	}}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 5, SizeBytes: 8},
			{PartIndex: 2, Filename: "vod-42-02.m4a", DurationSeconds: 15, SizeBytes: 8},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeWaveform(t, resp.Body)
	if body.DurationSeconds != 20 {
		t.Fatalf("duration = %v, want 20", body.DurationSeconds)
	}
	if len(body.Peaks) != 160 {
		t.Fatalf("peaks len = %d, want 160", len(body.Peaks))
	}
	if len(generator.calls) != 2 {
		t.Fatalf("generator calls = %d, want 2", len(generator.calls))
	}
	if generator.calls[0].points != 41 || generator.calls[1].points != 119 {
		t.Fatalf("points = %d/%d, want 41/119", generator.calls[0].points, generator.calls[1].points)
	}
	if body.Peaks[0] != 0.1 || body.Peaks[41] != 0.2 {
		t.Fatalf("multipart peaks did not append part values at boundary: first=%v boundary=%v", body.Peaks[0], body.Peaks[41])
	}
}

func TestAudioWaveformPersistsArtifactByPartFingerprint(t *testing.T) {
	store := &signedStorage{bodies: map[string][]byte{
		"videos/vod-42-01.m4a": []byte("audio-part"),
	}}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	for range 2 {
		resp := getWaveform(t, srv.URL, 42)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want single generated artifact", len(generator.calls))
	}
	if _, ok := store.bodies[storagekeys.Waveform("vod-42")]; !ok {
		t.Fatalf("stored bodies missing waveform artifact %s", storagekeys.Waveform("vod-42"))
	}
}

func TestAudioWaveformRebuildsWhenS3ArtifactIsMissing(t *testing.T) {
	store := &signedStorage{
		bodies: map[string][]byte{
			"videos/vod-42-01.m4a": []byte("audio-part"),
		},
		openErrs: map[string][]error{
			storagekeys.Waveform("vod-42"): {fsNotExistOnlyError{path: storagekeys.Waveform("vod-42")}},
		},
	}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(generator.calls) != 1 {
		t.Fatalf("generator calls = %d, want rebuild after missing artifact", len(generator.calls))
	}
	if _, ok := store.bodies[storagekeys.Waveform("vod-42")]; !ok {
		t.Fatalf("stored bodies missing rebuilt waveform artifact %s", storagekeys.Waveform("vod-42"))
	}
}

func TestAudioWaveformMissingS3PartReturnsNotFound(t *testing.T) {
	store := &signedStorage{
		bodies: map[string][]byte{},
		openErrs: map[string][]error{
			storagekeys.Video("vod-42-01.m4a"): {fsNotExistOnlyError{path: storagekeys.Video("vod-42-01.m4a")}},
		},
	}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0 when input part is missing", len(generator.calls))
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestAudioWaveformStorageErrorReturnsServerError(t *testing.T) {
	store := &signedStorage{
		bodies: map[string][]byte{
			"videos/vod-42-01.m4a": []byte("audio-part"),
		},
		openErrs: map[string][]error{
			storagekeys.Waveform("vod-42"): {errors.New("storage unavailable")},
		},
	}
	video := doneVideo()
	video.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{
		video: video,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 10},
		},
	}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithWaveformGenerator(generator))

	resp := getWaveform(t, srv.URL, 42)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if len(generator.calls) != 0 {
		t.Fatalf("generator calls = %d, want 0 on artifact storage error", len(generator.calls))
	}
}
