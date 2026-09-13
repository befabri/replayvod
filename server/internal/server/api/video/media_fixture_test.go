package video

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

// WithStorageGate sets fixture readiness before HTTP requests begin.
func WithStorageGate(g mediastore.Gate) StreamHandlerOption {
	return func(h *StreamHandler) { mediatest.SetGate(h.storage, g) }
}
func WithRecordingLocks(l *recordinglock.Locks) StreamHandlerOption {
	return func(h *StreamHandler) { h.storage = mediatest.WithLocks(h.storage, l) }
}

func streamMedia(t *testing.T, repo repository.Repository, raw storage.Storage, gate mediastore.Gate, locks *recordinglock.Locks) *mediastore.Store {
	if fake, ok := repo.(*signedRepo); ok && fake.Repository == nil {
		db := testdb.NewSQLiteDB(t)
		fake.Repository = sqliteadapter.New(db)
		// Artifact references use real foreign keys even when row reads are
		// fault-injected by signedRepo. Seed their owner before serving requests.
		if fake.video != nil {
			if _, err := db.ExecContext(t.Context(), `INSERT INTO channels(broadcaster_id, broadcaster_login, broadcaster_name) VALUES('media-fixture','media-fixture','Media fixture')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status) VALUES(?,?,?,?,?,?)`, fake.video.ID, "fixture-job", fake.video.Filename, "Media fixture", "media-fixture", fake.video.Status); err != nil {
				t.Fatal(err)
			}
		}
	}
	return mediatest.New(t, repo, raw, gate, locks)
}

type streamTx struct {
	repository.Repository
	owner *signedRepo
}

func (r streamTx) GetVideoForUpdate(ctx context.Context, id int64) (*repository.Video, error) {
	return r.owner.GetVideo(ctx, id)
}
func (r *signedRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(streamTx{tx, r}) })
}
