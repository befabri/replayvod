package video

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"

	"github.com/befabri/replayvod/server/internal/background"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/waveform"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	waveformMinPoints = waveform.MinPoints
)

type WaveformGenerator = waveform.Generator

type AudioWaveformResponse = waveform.Response

type waveformFlights struct {
	runner *background.Runner
	mu     sync.Mutex
	calls  map[string]*waveformFlight
}

type waveformFlight struct {
	done   chan struct{}
	resp   AudioWaveformResponse
	status int
	err    error
}

func newWaveformFlights() *waveformFlights {
	return &waveformFlights{runner: background.New(nil), calls: make(map[string]*waveformFlight)}
}

func (f *waveformFlights) Do(ctx context.Context, key string, fn func(context.Context) (AudioWaveformResponse, int, error)) (AudioWaveformResponse, int, error) {
	f.mu.Lock()
	if call := f.calls[key]; call != nil {
		f.mu.Unlock()
		return waitForWaveformFlight(ctx, call)
	}
	call := &waveformFlight{done: make(chan struct{})}
	f.calls[key] = call
	err := f.runner.Start("waveform", key+"/"+uuid.NewString(), func(buildCtx context.Context) error {
		call.resp, call.status, call.err = fn(buildCtx)
		return call.err
	}, func(runErr error) {
		if runErr != nil && call.err == nil {
			call.err, call.status = runErr, http.StatusInternalServerError
		}
		close(call.done)
		f.mu.Lock()
		if f.calls[key] == call {
			delete(f.calls, key)
		}
		f.mu.Unlock()
	})
	if err != nil {
		delete(f.calls, key)
		f.mu.Unlock()
		return AudioWaveformResponse{}, http.StatusInternalServerError, err
	}
	f.mu.Unlock()
	return waitForWaveformFlight(ctx, call)
}

func (f *waveformFlights) Close(ctx context.Context) error {
	f.runner.Stop()
	return f.runner.Wait(ctx)
}

// Close rejects new waveform work and waits up to 30 seconds for active work.
func (h *StreamHandler) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.waveformFlights.Close(ctx)
}

func waitForWaveformFlight(ctx context.Context, call *waveformFlight) (AudioWaveformResponse, int, error) {
	select {
	case <-call.done:
		return call.resp, call.status, call.err
	case <-ctx.Done():
		return AudioWaveformResponse{}, statusClientClosed, ctx.Err()
	}
}

func (h *StreamHandler) streamAudioWaveform(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid video id", http.StatusBadRequest)
		return
	}

	resp, status, err := h.audioWaveform(r.Context(), id)
	if err != nil {
		if clientGone(r.Context(), err) {
			http.Error(w, "client closed request", statusClientClosed)
			return
		}
		h.log.Error("audio waveform failed", "error", err, "id", id)
		http.Error(w, http.StatusText(status), status)
		return
	}
	if status != http.StatusOK {
		http.Error(w, http.StatusText(status), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Warn("audio waveform response encode failed", "error", err, "id", id)
	}
}

func (h *StreamHandler) audioWaveform(ctx context.Context, id int64) (AudioWaveformResponse, int, error) {
	video, err := h.repo.GetVideo(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return AudioWaveformResponse{}, http.StatusNotFound, nil
		}
		return AudioWaveformResponse{}, http.StatusInternalServerError, err
	}
	if video.Status != repository.VideoStatusDone {
		return AudioWaveformResponse{}, http.StatusNotFound, nil
	}
	if video.DeletedAt != nil {
		return AudioWaveformResponse{}, http.StatusGone, nil
	}

	parts, err := h.repo.ListVideoParts(ctx, id)
	if err != nil {
		return AudioWaveformResponse{}, http.StatusInternalServerError, err
	}
	if !isAudioOnlyRecording(video, parts) {
		return AudioWaveformResponse{}, http.StatusNotFound, nil
	}

	plan, ok := buildWaveformPlan(video, parts)
	if !ok {
		return AudioWaveformResponse{}, http.StatusNotFound, nil
	}
	if err := h.storageUnavailable(); err != nil {
		return AudioWaveformResponse{}, http.StatusServiceUnavailable, err
	}
	if resp, hit, err := waveform.LoadRecording(ctx, h.storage, id, plan.Fingerprint); err != nil {
		return AudioWaveformResponse{}, waveformErrorStatus(err), err
	} else if hit {
		return resp, http.StatusOK, nil
	}

	return h.waveformFlights.Do(ctx, plan.Fingerprint, func(buildCtx context.Context) (AudioWaveformResponse, int, error) {
		if err := h.storageUnavailable(); err != nil {
			return AudioWaveformResponse{}, http.StatusServiceUnavailable, err
		}
		if resp, hit, err := waveform.LoadRecording(buildCtx, h.storage, id, plan.Fingerprint); err != nil {
			return AudioWaveformResponse{}, waveformErrorStatus(err), err
		} else if hit {
			return resp, http.StatusOK, nil
		}

		if err := h.verifyStorage(buildCtx); err != nil {
			return AudioWaveformResponse{}, http.StatusServiceUnavailable, err
		}
		resp, err := waveform.Generate(buildCtx, h.waveformGenerator, waveform.InputResolver{Storage: h.storage}, plan)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return AudioWaveformResponse{}, http.StatusNotFound, nil
			}
			return AudioWaveformResponse{}, waveformErrorStatus(err), err
		}
		unlock, err := h.storage.Lock(buildCtx, id)
		if err != nil {
			return AudioWaveformResponse{}, http.StatusInternalServerError, err
		}
		defer unlock.Close()
		fresh, err := h.repo.GetVideo(buildCtx, id)
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return AudioWaveformResponse{}, http.StatusNotFound, nil
		case err != nil:
			return AudioWaveformResponse{}, http.StatusInternalServerError, err
		case fresh.DeletedAt != nil:
			return AudioWaveformResponse{}, http.StatusGone, nil
		case fresh.Status != repository.VideoStatusDone:
			return AudioWaveformResponse{}, http.StatusNotFound, nil
		}
		if err := h.verifyStorage(buildCtx); err != nil {
			return AudioWaveformResponse{}, http.StatusServiceUnavailable, err
		}
		if err := waveform.SaveArtifact(buildCtx, unlock, fresh.Filename, plan.Fingerprint, resp); err != nil {
			return AudioWaveformResponse{}, waveformErrorStatus(err), err
		}
		return resp, http.StatusOK, nil
	})
}

func isAudioOnlyRecording(video *repository.Video, parts []repository.VideoPart) bool {
	if repository.NormalizeRecordingType(video.RecordingType) == repository.RecordingTypeAudio {
		return true
	}
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if contentTypeForRecordingFile(part.Filename) == "audio/mp4" {
			continue
		}
		if part.Codec == "aac" || part.Quality == "audio_only" {
			continue
		}
		return false
	}
	return true
}

func buildWaveformPlan(video *repository.Video, parts []repository.VideoPart) (waveform.Plan, bool) {
	inputs := make([]waveform.PartInput, len(parts))
	for i, part := range parts {
		inputs[i] = waveform.PartInput{
			Filename:        part.Filename,
			DurationSeconds: part.DurationSeconds,
			SizeBytes:       part.SizeBytes,
		}
	}
	return waveform.BuildPlan(video.ID, video.RecordingType, video.DurationSeconds, inputs)
}

func waveformErrorStatus(err error) int {
	for _, cause := range []error{storage.ErrUnattached, storage.ErrUnreachable, storage.ErrReadOnly, storage.ErrFull} {
		if errors.Is(err, cause) {
			return http.StatusServiceUnavailable
		}
	}
	return http.StatusInternalServerError
}
