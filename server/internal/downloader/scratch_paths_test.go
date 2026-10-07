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

	"github.com/befabri/replayvod/server/internal/repository"
)

func attemptScratchPath(t *testing.T, s *Service, d *download, name string) string {
	t.Helper()
	dir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

func TestRecordingRejectsBroadcasterPathInjection(t *testing.T) {
	for _, login := range []string{"../../outside", "a/../../../outside", `a\..\outside`, "/absolute", "line\nbreak", "nul\x00byte"} {
		t.Run(login, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			p := Params{BroadcasterID: "webhook", BroadcasterLogin: login, VODID: "123"}
			if _, err := s.Start(t.Context(), p); !errors.Is(err, errInvalidRecordingPath) {
				t.Fatalf("Start accepted unsafe login: %v", err)
			}
			if _, err := s.EnqueueVOD(t.Context(), p); !errors.Is(err, errInvalidRecordingPath) {
				t.Fatalf("archive accepted unsafe login: %v", err)
			}
			d := &download{resume: NewResumeState()}
			if _, err := s.runPart(t.Context(), t.Context(), d, p, buildFilename(login, "job"), "unused", nil, nil, s.log); !errors.Is(err, errInvalidRecordingPath) {
				t.Fatalf("part reached filesystem work with unsafe name: %v", err)
			}
			entries, err := os.ReadDir(s.cfg.Env.ScratchDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected recording wrote scratch entries: %v, %v", entries, err)
			}
		})
	}
	for _, login := range []string{"channel_123", "harness-live", ""} {
		if err := validateRecordingName(buildFilename(login, "job")); err != nil {
			t.Fatalf("ordinary login %q rejected: %v", login, err)
		}
	}
}

func TestPreparedScratchReadsRejectEscapePaths(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "read-owner")
	inside := attemptScratchPath(t, s, d, "prepared.mp4")
	const media = "owned recording"
	if err := os.WriteFile(inside, []byte(media), 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(media))
	digest, err := s.preparedDigest(t.Context(), d, inside)
	if err != nil || digest != hex.EncodeToString(want[:]) {
		t.Fatalf("owned scratch digest: %q, %v", digest, err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(s.cfg.Env.ScratchDir, "another-job", "private.txt")
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
	paths := map[string]string{
		"absolute outside":  outside,
		"parent traversal":  filepath.Dir(inside) + "/../another-job/private.txt",
		"other attempt":     sibling,
		"file symlink":      fileLink,
		"directory symlink": filepath.Join(dirLink, "private.txt"),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			if got, err := s.preparedDigest(t.Context(), d, path); !errors.Is(err, errInvalidRecordingPath) || got != "" {
				t.Fatalf("escaped scratch was hashed: %q, %v", got, err)
			}
			if err := s.uploadFromScratch(t.Context(), d, path, "videos/leaked.mp4"); !errors.Is(err, errInvalidRecordingPath) {
				t.Fatalf("escaped scratch was uploaded: %v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err := s.publishPreparedPart(ctx, d, &PreparedPart{Filename: "leaked.mp4", Path: path, Digest: digest}, s.log)
			if !errors.Is(err, errInvalidRecordingPath) || ctx.Err() != nil {
				t.Fatalf("invalid persisted path was retried instead of rejected: %v", err)
			}
			if exists, err := s.storage.Exists(t.Context(), "videos/leaked.mp4"); err != nil || exists {
				t.Fatalf("escaped input published: exists=%v, %v", exists, err)
			}
		})
	}
}

func TestPreparedScratchRejectsSymlinkedJobDirectory(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := &download{jobID: "linked-job"}
	other := filepath.Join(s.cfg.Env.ScratchDir, "other-job")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "private.mp4"), []byte("other attempt"), 0o600); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	if err := os.Symlink(other, jobDir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.preparedDigest(t.Context(), d, filepath.Join(jobDir, "private.mp4")); !errors.Is(err, errInvalidRecordingPath) {
		t.Fatalf("symlinked job directory accepted: %v", err)
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

func TestScratchJobRejectsReplacementAfterStat(t *testing.T) {
	for _, replacement := range []string{"symlink to sibling", "different directory"} {
		t.Run(replacement, func(t *testing.T) {
			dir := t.TempDir()
			jobDir := filepath.Join(dir, "job")
			if err := os.Mkdir(jobDir, 0o755); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			expected, err := root.Lstat("job")
			if err != nil {
				t.Fatal(err)
			}
			unchanged, err := openScratchJob(root, "job", expected)
			if err != nil {
				t.Fatalf("unchanged job rejected: %v", err)
			}
			unchanged.Close()
			// Reproduce replacement in the window after Lstat, without a racy
			// timing loop or a hook in the production filesystem operations.
			if err := os.Rename(jobDir, jobDir+"-original"); err != nil {
				t.Fatal(err)
			}
			if replacement == "symlink to sibling" {
				if err := os.Mkdir(filepath.Join(dir, "sibling"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("sibling", jobDir); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(jobDir, 0o755); err != nil {
				t.Fatal(err)
			}
			opened, err := openScratchJob(root, "job", expected)
			if opened != nil {
				opened.Close()
				t.Fatal("replacement directory was returned for scratch reads")
			}
			if !errors.Is(err, errInvalidRecordingPath) {
				t.Fatalf("replacement directory accepted: %v", err)
			}
		})
	}
}
