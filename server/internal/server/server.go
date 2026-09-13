package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api"
	"github.com/befabri/replayvod/server/internal/service/playbackcache"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type Server struct {
	cfg           *config.Config
	repo          repository.Repository
	sessionMgr    *session.Manager
	twitchClient  *twitch.Client
	storage       storage.Storage
	downloader    *downloader.Service
	hydrator      *streammeta.Hydrator
	bus           *eventbus.Buses
	processor     *schedulesvc.EventProcessor
	webhook       *recordingwebhook.Dispatcher
	playbackCache *playbackcache.Service
	log           *slog.Logger
	httpServer    *http.Server
	closeRouter   func() error
	recordings    *api.RecordingServices
}

// NewServer requires the shared recording services used by background workers.
// A nil bus closes subscription feeds immediately. Share hydrator with the
// downloader so HTTP and recording workers use the same metadata cache.
func NewServer(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, store storage.Storage, dl *downloader.Service, hydrator *streammeta.Hydrator, bus *eventbus.Buses, processor *schedulesvc.EventProcessor, webhook *recordingwebhook.Dispatcher, playbackCache *playbackcache.Service, log *slog.Logger, recordings *api.RecordingServices) *Server {
	if recordings == nil || recordings.Media == nil {
		panic("shared recording services required")
	}
	return &Server{
		cfg:           cfg,
		recordings:    recordings,
		repo:          repo,
		sessionMgr:    sessionMgr,
		twitchClient:  twitchClient,
		storage:       store,
		downloader:    dl,
		hydrator:      hydrator,
		bus:           bus,
		processor:     processor,
		webhook:       webhook,
		playbackCache: playbackCache,
		log:           log,
	}
}

// Start binds the listener and reports its result to ready without blocking.
// Use a buffered channel to receive readiness before starting shutdown.
func (s *Server) Start(ready chan<- error) {
	router, closeRouter := api.SetupRouter(s.cfg, s.repo, s.sessionMgr, s.twitchClient, s.storage, s.downloader, s.hydrator, s.bus, s.processor, s.webhook, s.playbackCache, s.log, s.recordings)
	s.closeRouter = closeRouter
	addr := s.cfg.GetAddress()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		notifyReady(ready, err)
		s.log.Error("Server listen error", "error", err)
		return
	}

	// A global WriteTimeout would truncate SSE streams and large downloads.
	// Use per-route deadlines for bounded responses.
	s.httpServer = &http.Server{
		Addr:        addr,
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
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
	if s.closeRouter != nil {
		if err := s.closeRouter(); err != nil {
			s.log.Error("Router shutdown error", "error", err)
		}
		s.closeRouter = nil
	}
}
