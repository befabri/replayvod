package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/trpcgo"
)

func requireTRPCCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want tRPC code %v", want)
	}
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("error = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != want {
		t.Fatalf("tRPC code = %v, want %v", te.Code, want)
	}
}

func newHandler(t *testing.T) *Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	return NewHandler(New(repo, log), log)
}

func TestList_Empty(t *testing.T) {
	h := newHandler(t)
	got, err := h.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if got.Data == nil {
		t.Fatal("Data is nil, want non-nil slice")
	}
	if len(got.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0", len(got.Data))
	}
}

func TestToggle_NotFound(t *testing.T) {
	h := newHandler(t)
	_, err := h.Toggle(context.Background(), ToggleInput{Name: "no-such-task", Enabled: true})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

func TestRunNow_NotFound(t *testing.T) {
	h := newHandler(t)
	_, err := h.RunNow(context.Background(), RunNowInput{Name: "no-such-task"})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

func TestToResponse_FieldMapping(t *testing.T) {
	errMsg := "last error text"
	task := &repository.Task{
		Name:            "my-task",
		Description:     "desc",
		IntervalSeconds: 60,
		IsEnabled:       true,
		LastDurationMs:  123,
		LastStatus:      repository.TaskStatusSuccess,
		LastError:       &errMsg,
	}
	r := toResponse(task)
	if r.Name != task.Name {
		t.Errorf("Name: %q != %q", r.Name, task.Name)
	}
	if r.Description != task.Description {
		t.Errorf("Description: %q != %q", r.Description, task.Description)
	}
	if r.IntervalSeconds != task.IntervalSeconds {
		t.Errorf("IntervalSeconds: %d != %d", r.IntervalSeconds, task.IntervalSeconds)
	}
	if !r.IsEnabled {
		t.Errorf("IsEnabled: false, want true")
	}
	if r.LastDurationMs != task.LastDurationMs {
		t.Errorf("LastDurationMs: %d != %d", r.LastDurationMs, task.LastDurationMs)
	}
	if r.LastStatus != task.LastStatus {
		t.Errorf("LastStatus: %q != %q", r.LastStatus, task.LastStatus)
	}
	if r.LastError == nil || *r.LastError != errMsg {
		t.Errorf("LastError: %v, want %q", r.LastError, errMsg)
	}
}
