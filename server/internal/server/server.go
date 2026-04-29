package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// Server represents the HTTP server.
type Server struct {
	cfg          *config.Config
	repo         repository.Repository
	sessionMgr   *session.Manager
	twitchClient *twitch.Client
	storage      storage.Storage
	downloader   *downloader.Service
	hydrator     *streammeta.Hydrator
	bus          *eventbus.Buses
	log          *slog.Logger
	httpServer   *http.Server
	closeTRPC    func() error
}

// NewServer creates a new server. bus may be nil to disable SSE
// feeds — the subscription procedures then return pre-closed channels.
// hydrator is shared with the downloader's MetadataWatcher so routes and
// internal polling agree on the Helix-derived view.
func NewServer(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, store storage.Storage, dl *downloader.Service, hydrator *streammeta.Hydrator, bus *eventbus.Buses, log *slog.Logger) *Server {
	return &Server{
		cfg:          cfg,
		repo:         repo,
		sessionMgr:   sessionMgr,
		twitchClient: twitchClient,
		storage:      store,
		downloader:   dl,
		hydrator:     hydrator,
		bus:          bus,
		log:          log,
	}
}

// Start begins serving HTTP requests. If ready is non-nil, it receives nil
// after the TCP listener is bound or an error if the server cannot listen.
func (s *Server) Start(ready chan<- error) {
	router, closeTRPC := api.SetupRouter(s.cfg, s.repo, s.sessionMgr, s.twitchClient, s.storage, s.downloader, s.hydrator, s.bus, s.log)
	s.closeTRPC = closeTRPC
	addr := fmt.Sprintf("%s:%d", s.cfg.Env.Host, s.cfg.Env.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		notifyReady(ready, err)
		s.log.Error("Server listen error", "error", err)
		return
	}

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	notifyReady(ready, nil)
	if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		s.log.Error("Server error", "error", err)
	}
}

func notifyReady(ready chan<- error, err error) {
	if ready == nil {
		return
	}
	select {
	case ready <- err:
	default:
	}
}

// Stop gracefully shuts down the server. Active downloads are cancelled
// first so the HTTP shutdown doesn't outrun subprocess termination.
func (s *Server) Stop() {
	if s.downloader != nil {
		s.downloader.Shutdown()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.log.Error("Server shutdown error", "error", err)
		} else {
			s.log.Info("Server gracefully stopped")
		}
	}
	if s.closeTRPC != nil {
		if err := s.closeTRPC(); err != nil {
			s.log.Error("tRPC router shutdown error", "error", err)
		}
		s.closeTRPC = nil
	}
}
