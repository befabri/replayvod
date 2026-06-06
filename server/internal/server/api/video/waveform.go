package video

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"sync"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/waveform"
	"github.com/go-chi/chi/v5"
)

const (
	waveformMinPoints = waveform.MinPoints
)

type WaveformGenerator = waveform.Generator

type AudioWaveformResponse = waveform.Response

type waveformFlights struct {
	mu    sync.Mutex
	calls map[string]*waveformFlight
}

type waveformFlight struct {
	done   chan struct{}
	resp   AudioWaveformResponse
	status int
	err    error
}

func newWaveformFlights() *waveformFlights {
	return &waveformFlights{calls: make(map[string]*waveformFlight)}
}

func (f *waveformFlights) Do(ctx context.Context, key string, fn func(context.Context) (AudioWaveformResponse, int, error)) (AudioWaveformResponse, int, error) {
	f.mu.Lock()
	if call := f.calls[key]; call != nil {
		f.mu.Unlock()
		return waitForWaveformFlight(ctx, call)
	}
	call := &waveformFlight{done: make(chan struct{})}
	f.calls[key] = call
	f.mu.Unlock()

	go f.run(ctx, key, call, fn)

	return waitForWaveformFlight(ctx, call)
}

func (f *waveformFlights) run(ctx context.Context, key string, call *waveformFlight, fn func(context.Context) (AudioWaveformResponse, int, error)) {
	call.resp, call.status, call.err = fn(context.WithoutCancel(ctx))
	close(call.done)

	f.mu.Lock()
	if f.calls[key] == call {
		delete(f.calls, key)
	}
	f.mu.Unlock()
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
		if clientGone(err) {
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
	key := storagekeys.Waveform(video.Filename)
	if resp, hit, err := waveform.LoadArtifact(ctx, h.storage, key, plan.Fingerprint); err != nil {
		return AudioWaveformResponse{}, http.StatusInternalServerError, err
	} else if hit {
		return resp, http.StatusOK, nil
	}

	return h.waveformFlights.Do(ctx, plan.Fingerprint, func(buildCtx context.Context) (AudioWaveformResponse, int, error) {
		if resp, hit, err := waveform.LoadArtifact(buildCtx, h.storage, key, plan.Fingerprint); err != nil {
			return AudioWaveformResponse{}, http.StatusInternalServerError, err
		} else if hit {
			return resp, http.StatusOK, nil
		}

		resp, err := waveform.Generate(buildCtx, h.waveformGenerator, waveform.InputResolver{Storage: h.storage}, plan)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return AudioWaveformResponse{}, http.StatusNotFound, nil
			}
			return AudioWaveformResponse{}, http.StatusInternalServerError, err
		}
		if err := waveform.SaveArtifact(buildCtx, h.storage, key, plan.Fingerprint, resp); err != nil {
			return AudioWaveformResponse{}, http.StatusInternalServerError, err
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
