package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

type lookupRepo struct {
	repository.Repository
	sess    *repository.Session
	err     error
	hash    string
	deleted string
}

func (r *lookupRepo) GetSession(_ context.Context, hash string) (*repository.Session, error) {
	r.hash = hash
	return r.sess, r.err
}

func (r *lookupRepo) DeleteSession(_ context.Context, hash string) error {
	r.deleted = hash
	return nil
}

func TestGetSession(t *testing.T) {
	outage := errors.New("database unavailable")
	live := &repository.Session{ExpiresAt: time.Now().Add(time.Hour)}
	expired := &repository.Session{ExpiresAt: time.Now().Add(-time.Hour)}
	for _, tc := range []struct {
		name         string
		cookie       bool
		sess         *repository.Session
		err, wantErr error
		want         *repository.Session
		deleted      bool
	}{
		{name: "no cookie"},
		{name: "live", cookie: true, sess: live, want: live},
		{name: "expired", cookie: true, sess: expired, deleted: true},
		{name: "missing", cookie: true, err: repository.ErrNotFound},
		{name: "wrapped missing", cookie: true, err: fmt.Errorf("lookup: %w", repository.ErrNotFound)},
		{name: "outage", cookie: true, err: outage, wantErr: outage},
		{name: "canceled", cookie: true, err: context.Canceled, wantErr: context.Canceled},
		{name: "deadline", cookie: true, err: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &lookupRepo{sess: tc.sess, err: tc.err}
			mgr := &Manager{repo: repo}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: CookieName, Value: "raw-session-id"})
			}
			got, err := mgr.Get(r.Context(), r)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Get = %v, %v; want %v, %v", got, err, tc.want, tc.wantErr)
			}
			if tc.cookie && repo.hash != HashSessionID("raw-session-id") || !tc.cookie && repo.hash != "" {
				t.Fatalf("unexpected lookup hash %q", repo.hash)
			}
			if (repo.deleted != "") != tc.deleted || tc.deleted && repo.deleted != repo.hash {
				t.Fatalf("unexpected session deletion %q", repo.deleted)
			}
		})
	}
}
