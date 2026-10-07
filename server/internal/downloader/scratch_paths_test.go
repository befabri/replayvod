package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// attemptScratchPath opens the attempt's workspace when the test has not run
// capture, and returns a path inside it.
func attemptScratchPath(t *testing.T, s *Service, d *download, name string) string {
	t.Helper()
	if d.workspace == nil {
		workspace, err := s.storage.Scratch().Open(filepath.Join(s.storage.Scratch().Root(), d.jobID), 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = workspace.Close(true) })
		d.workspace = workspace
	}
	return filepath.Join(d.workspace.Dir, name)
}

func TestRecordingRejectsBroadcasterPathInjection(t *testing.T) {
	for _, login := range []string{"../../outside", "a/../../../outside", `a\..\outside`, "/absolute", "..", "dotted.name", "line\nbreak", "nul\x00byte"} {
		t.Run(login, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			p := Params{BroadcasterID: "webhook", BroadcasterLogin: login, VODID: "123"}
			if _, err := s.Start(t.Context(), p); !errors.Is(err, errInvalidRecordingName) {
				t.Fatalf("Start accepted unsafe login: %v", err)
			}
			if _, err := s.EnqueueVOD(t.Context(), p); !errors.Is(err, errInvalidRecordingName) {
				t.Fatalf("archive accepted unsafe login: %v", err)
			}
			entries, err := os.ReadDir(s.cfg.Env.ScratchDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected recording wrote scratch entries: %v, %v", entries, err)
			}
		})
	}
	for _, login := range []string{"channel_123", "harness-live", "MixedCase", ""} {
		name, err := buildFilename(login, "job")
		if err != nil || !validRecordingName(name) {
			t.Fatalf("ordinary login %q rejected: %q, %v", login, name, err)
		}
	}
}

func TestPreparedScratchReadsStayInsideWorkspace(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "read-owner")
	inside := attemptScratchPath(t, s, d, "prepared.mp4")
	const media = "owned recording"
	if err := os.WriteFile(inside, []byte(media), 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(media))
	digest, err := preparedDigest(t.Context(), d, inside)
	if err != nil || digest != hex.EncodeToString(want[:]) {
		t.Fatalf("owned scratch digest: %q, %v", digest, err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(s.storage.Scratch().Root(), "another-job", "private.txt")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("another recording"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileLink := attemptScratchPath(t, s, d, "external.mp4")
	if err := os.Symlink(outside, fileLink); err != nil {
		t.Fatal(err)
	}
	dirLink := attemptScratchPath(t, s, d, "external-dir")
	if err := os.Symlink(filepath.Dir(sibling), dirLink); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		path    string
		lexical bool
	}{
		"absolute outside":  {outside, true},
		"parent traversal":  {filepath.Dir(inside) + "/../another-job/private.txt", true},
		"other attempt":     {sibling, true},
		"file symlink":      {fileLink, false},
		"directory symlink": {filepath.Join(dirLink, "private.txt"), false},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := preparedDigest(t.Context(), d, tc.path); err == nil || got != "" {
				t.Fatalf("escaped scratch was hashed: %q, %v", got, err)
			}
			if err := s.uploadFromScratch(t.Context(), d, tc.path, "videos/leaked.mp4"); err == nil {
				t.Fatal("escaped scratch was uploaded")
			}
			if tc.lexical {
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				_, err := s.publishPreparedPart(ctx, d, &PreparedPart{Filename: "leaked.mp4", Path: tc.path, Digest: digest}, s.log)
				if !errors.Is(err, mediastore.ErrOutsideWorkspace) || ctx.Err() != nil {
					t.Fatalf("path outside the workspace was retried instead of rejected: %v", err)
				}
			}
			if exists, err := s.storage.Exists(t.Context(), "videos/leaked.mp4"); err != nil || exists {
				t.Fatalf("escaped input published: exists=%v, %v", exists, err)
			}
		})
	}
}

func TestPreparedPublicationRetriesTemporaryOpenFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "retry-open")
	output := attemptScratchPath(t, s, d, "prepared.mp4")
	if err := os.WriteFile(output, []byte("prepared bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := preparedDigest(t.Context(), d, output)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(output, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = s.publishPreparedPart(ctx, d, &PreparedPart{Filename: "prepared.mp4", Path: output, Digest: digest}, s.log)
	if !errors.Is(err, os.ErrPermission) || ctx.Err() == nil {
		t.Fatalf("temporary open failure ended publication instead of deferring it: %v", err)
	}
}

func TestPreparedPartRejectsForeignStorageKeys(t *testing.T) {
	for name, part := range map[string]PreparedPart{
		"thumbnail": {Thumbnail: storagekeys.Video("other-recording.mp4")},
		"strip":     {Strip: storagekeys.Thumbnail("other-recording")},
		"filename":  {Filename: "../other-recording.mp4"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "foreign-keys")
			if part.Filename == "" {
				part.Filename = "recording-part01.mp4"
			}
			part.Path = attemptScratchPath(t, s, d, "recording-part01.mp4")
			part.ThumbnailPath = attemptScratchPath(t, s, d, "recording-part01.jpg")
			part.StripPath = attemptScratchPath(t, s, d, "recording-part01-strip.jpg")
			for _, path := range []string{part.Path, part.ThumbnailPath, part.StripPath} {
				if err := os.WriteFile(path, []byte("prepared bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			digest, err := preparedDigest(t.Context(), d, part.Path)
			if err != nil {
				t.Fatal(err)
			}
			part.Digest = digest
			if _, err := s.publishPreparedPart(t.Context(), d, &part, s.log); !errors.Is(err, errInvalidResume) {
				t.Fatalf("checkpoint named another recording's objects: %v", err)
			}
			for _, key := range []string{storagekeys.Video("other-recording.mp4"), storagekeys.Thumbnail("other-recording"), storagekeys.Video("recording-part01.mp4")} {
				if exists, err := s.storage.Exists(t.Context(), key); err != nil || exists {
					t.Fatalf("%s written from a rejected checkpoint: exists=%v, %v", key, exists, err)
				}
			}
		})
	}
}

func TestRecoveryRejectsUnsafeStoredRecordingName(t *testing.T) {
	s := newTestService(t, t.TempDir())
	if _, err := s.repo.UpsertChannel(t.Context(), &repository.Channel{BroadcasterID: "webhook", BroadcasterLogin: "webhook", BroadcasterName: "Webhook"}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := NewResumeState().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	video, err := repository.CreateAttempt(t.Context(), s.repo, &repository.VideoInput{
		JobID: "unsafe-recovery", Filename: "../../outside", BroadcasterID: "webhook",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
	}, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.GetJob(t.Context(), video.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.reconstructAttempt(t.Context(), job); !errors.Is(err, errInvalidResume) {
		t.Fatalf("recovery accepted unsafe persisted name: %v", err)
	}
}
