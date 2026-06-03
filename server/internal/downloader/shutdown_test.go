package downloader

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestShutdownIgnoresActiveDownloadWithoutCancel(t *testing.T) {
	s := &Service{
		active: map[string]*download{
			"job-1": {jobID: "job-1", progressCh: make(chan Progress, 1)},
		},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.Shutdown()
}

func TestStartRejectsAfterShutdownBegins(t *testing.T) {
	s := &Service{
		active: map[string]*download{},
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.shuttingDown.Store(true)

	if _, err := s.Start(context.Background(), Params{}); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Start while shutting down err = %v, want ErrShuttingDown", err)
	}
}
