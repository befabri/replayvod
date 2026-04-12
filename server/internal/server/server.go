package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api"
)

// Server represents the HTTP server.
type Server struct {
	cfg        *config.Config
	repo       repository.Repository
	log        *slog.Logger
	httpServer *http.Server
}

// NewServer creates a new server.
func NewServer(cfg *config.Config, repo repository.Repository, log *slog.Logger) *Server {
	return &Server{
		cfg:  cfg,
		repo: repo,
		log:  log,
	}
}

// Start begins serving HTTP requests.
func (s *Server) Start() {
	router := api.SetupRouter(s.cfg, s.repo, s.log)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.cfg.Env.Host, s.cfg.Env.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.log.Error("Server error", "error", err)
	}
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.httpServer == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.Error("Server shutdown error", "error", err)
	} else {
		s.log.Info("Server gracefully stopped")
	}
}
