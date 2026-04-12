// Command gentrpc writes the TypeScript + Zod files under
// dashboard/src/api/generated/ by constructing the full tRPC router in
// non-dev mode and calling the library's explicit Generate APIs. Use it
// when you add a new procedure and need the types regenerated without
// running the full server (the server's dev watcher otherwise covers
// this, but the watcher needs a running DB).
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/server/api"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/migrations"
)

func main() {
	outDir := flag.String("out", "dashboard/src/api/generated", "output directory for trpc.ts + zod.ts")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Fresh SQLite tempfile — the router builder only needs a repo for
	// handler construction; nothing is ever invoked on it, so minimal
	// setup is enough.
	tmpPath := filepath.Join(os.TempDir(), "gentrpc.db")
	_ = os.Remove(tmpPath)
	defer os.Remove(tmpPath)
	db, err := database.NewSQLiteDB(tmpPath)
	if err != nil {
		fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if err := database.MigrateSQLite(context.Background(), db, migrations.SQLite()); err != nil {
		fatalf("migrate: %v", err)
	}
	repo := sqliteadapter.New(sqlitegen.New(db))

	cfg := &config.Config{}
	cfg.App.Development = false
	cfg.Env.WebhookCallbackURL = "https://example.invalid/api/v1/webhook/callback"
	cfg.Env.HMACSecret = "gentrpc-stub-secret"

	sessionMgr, err := session.NewManager(repo, "0123456789abcdef0123456789abcdef", false, log)
	if err != nil {
		fatalf("session manager: %v", err)
	}
	twitchClient := twitch.NewClient("stub", "stub", log)
	store, err := storage.NewLocal(os.TempDir())
	if err != nil {
		fatalf("storage: %v", err)
	}
	dl := downloader.NewService(cfg, repo, store, log)

	router := api.SetupTRPCRouter(cfg, repo, sessionMgr, twitchClient, dl, log)

	tsPath := filepath.Join(*outDir, "trpc.ts")
	zodPath := filepath.Join(*outDir, "zod.ts")
	if err := router.GenerateTS(tsPath); err != nil {
		fatalf("generate ts: %v", err)
	}
	if err := router.GenerateZod(zodPath); err != nil {
		fatalf("generate zod: %v", err)
	}
	os.Stdout.WriteString("wrote " + tsPath + "\nwrote " + zodPath + "\n")
}

func fatalf(f string, a ...any) {
	slog.Error("gentrpc failed", "err", f)
	_, _ = os.Stderr.WriteString("gentrpc: ")
	_, _ = os.Stderr.WriteString(f)
	_, _ = os.Stderr.WriteString("\n")
	if len(a) > 0 {
		slog.Error("args", "a", a)
	}
	os.Exit(1)
}
