package video

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestAudioWaveformStorageGate(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, verdict := range []struct {
			name string
			err  error
		}{
			{"unattached", storage.ErrUnattached},
			{"unreachable", storage.ErrUnreachable},
			{"read-only", storage.ErrReadOnly},
			{"full", storage.ErrFull},
		} {
			name := verdict.name
			if cached {
				name += "/cached"
			}
			t.Run(name, func(t *testing.T) {
				v := doneVideo()
				v.RecordingType = repository.RecordingTypeAudio
				repo := &signedRepo{video: v, parts: []repository.VideoPart{
					{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 5},
				}}
				store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-01.m4a": []byte("audio")}}
				generator := &fakeWaveformGenerator{}
				// Exercise generation first to create a real, fingerprint-matching
				// artifact; switch the gate only after the request has completed.
				var gateErr error
				gate := gateFunc(func() error { return gateErr })
				h := NewStreamHandler(repo, store, nil, testClientLogger(), WithStorageGate(gate), WithWaveformGenerator(generator))
				if cached {
					if _, status, err := h.audioWaveform(t.Context(), v.ID); err != nil || status != http.StatusOK {
						t.Fatalf("seed cached waveform: status=%d err=%v", status, err)
					}
				}
				gateErr = verdict.err
				store.opened = nil
				before := len(generator.calls)
				_, status, _ := h.audioWaveform(t.Context(), v.ID)
				want := http.StatusServiceUnavailable
				if cached && storage.CanRead(verdict.err) {
					want = http.StatusOK
				}
				if status != want {
					t.Errorf("status=%d, want %d", status, want)
				}
				if len(generator.calls) != before {
					t.Error("generated waveform while storage was unavailable for writes")
				}
				if !cached && store.bodies[storagekeys.Waveform(v.Filename)] != nil {
					t.Error("wrote a waveform despite refused storage")
				}
				if !storage.CanRead(verdict.err) && len(store.opened) != 0 {
					t.Errorf("read refused storage: %v", store.opened)
				}
			})
		}
	}
}

func TestAudioWaveformRefusesForeignStorage(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	expected := strings.Repeat("a", 64)
	if err := storage.WriteMarker(ctx, store, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "videos/vod-42-01.m4a", strings.NewReader("foreign audio")); err != nil {
		t.Fatal(err)
	}
	gate := gateFunc(func() error { return storage.Ready(ctx, store, expected) })
	if err := gate.Ready(); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("fixture not foreign: %v", err)
	}
	v := doneVideo()
	v.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{video: v, parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 13}}}
	generator := &fakeWaveformGenerator{}
	srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithStorageGate(gate), WithWaveformGenerator(generator))
	resp := getWaveform(t, srv.URL, v.ID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", resp.StatusCode)
	}
	if exists, err := store.Exists(ctx, storagekeys.Waveform(v.Filename)); err != nil || exists {
		t.Errorf("foreign storage was modified: waveform=%v err=%v", exists, err)
	}
	if len(generator.calls) != 0 {
		t.Error("generated waveform from foreign audio")
	}
}

type storageChangeWaveformGenerator struct {
	fakeWaveformGenerator
	afterGenerate func()
}

func (g *storageChangeWaveformGenerator) Generate(ctx context.Context, path string, duration float64, points int) ([]float32, error) {
	peaks, err := g.fakeWaveformGenerator.Generate(ctx, path, duration, points)
	g.afterGenerate()
	return peaks, err
}

func TestAudioWaveformRechecksStorageBeforeSaving(t *testing.T) {
	v := doneVideo()
	v.RecordingType = repository.RecordingTypeAudio
	repo := &signedRepo{video: v, parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 5}}}
	store := &signedStorage{bodies: map[string][]byte{"videos/vod-42-01.m4a": []byte("audio")}}
	var gateErr error
	generator := &storageChangeWaveformGenerator{afterGenerate: func() { gateErr = storage.ErrUnattached }}
	h := NewStreamHandler(repo, store, nil, testClientLogger(), WithStorageGate(gateFunc(func() error { return gateErr })), WithWaveformGenerator(generator))
	_, status, _ := h.audioWaveform(t.Context(), v.ID)
	if status != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", status)
	}
	if store.bodies[storagekeys.Waveform(v.Filename)] != nil {
		t.Error("saved waveform after storage became unattached")
	}
}

func TestAudioWaveformRejectsStorageSwapBeforeMonitorRefresh(t *testing.T) {
	for _, duringGeneration := range []bool{false, true} {
		name := "before generation"
		if duringGeneration {
			name = "during generation"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			monitor := storagehealth.New(sqliteadapter.New(testdb.NewSQLiteDB(t)), store, nil, testClientLogger(), "local", store.Root)
			if _, err := monitor.Attach(ctx); err != nil {
				t.Fatal(err)
			}
			v := doneVideo()
			v.RecordingType = repository.RecordingTypeAudio
			repo := &signedRepo{video: v, parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.m4a", DurationSeconds: 2, SizeBytes: 5}}}
			if err := store.Save(ctx, "videos/vod-42-01.m4a", strings.NewReader("audio")); err != nil {
				t.Fatal(err)
			}
			swap := func() {
				if err := storage.WriteMarker(ctx, store, strings.Repeat("b", 64)); err != nil {
					t.Error(err)
				}
				if err := monitor.Ready(); err != nil {
					t.Errorf("expected unchanged cached readiness, got %v", err)
				}
			}
			generator := &storageChangeWaveformGenerator{afterGenerate: func() {}}
			if duringGeneration {
				generator.afterGenerate = swap
			} else {
				swap()
			}
			srv := streamRouteTestServer(t, repo, store, testClientLogger(), WithStorageGate(monitor), WithWaveformGenerator(generator))
			resp := getWaveform(t, srv.URL, v.ID)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("waveform status=%d, want 503", resp.StatusCode)
			}
			if exists, err := store.Exists(ctx, storagekeys.Waveform(v.Filename)); err != nil || exists {
				t.Errorf("foreign storage modified: waveform=%v err=%v", exists, err)
			}
			if !duringGeneration && len(generator.calls) != 0 {
				t.Error("generated from a foreign volume before refreshing readiness")
			}
		})
	}
}
