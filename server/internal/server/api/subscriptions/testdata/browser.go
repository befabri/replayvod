// Browser-suite fixture: real HTTP/1.1 and subscription transport, synthetic data.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/server/api/subscriptions"
	"github.com/befabri/trpcgo"
)

func main() {
	router := trpcgo.NewRouter()
	var mu sync.Mutex
	state := "attached"
	feeds := make(map[string]int)
	connections := 0
	opened := 0
	samples := 0
	expireAfterCheck := false
	sessionExpired := false
	refusedHandshakes := 0
	expiredSessionChecks := 0
	topic := eventbus.NewTopic[map[string]any](8)
	trpcgo.MustVoidSubscribe(router, "storage.statusLive", func(ctx context.Context) (<-chan map[string]any, error) {
		mu.Lock()
		feeds["storage.statusLive"]++
		mu.Unlock()
		go func() { <-ctx.Done(); mu.Lock(); feeds["storage.statusLive"]--; mu.Unlock() }()
		return topic.Subscribe(ctx), nil
	})
	for _, path := range []string{"stream.live", "stream.status", "video.activeDownloadsLive", "archive.queueLive", "video.removalsLive"} {
		trpcgo.MustVoidSubscribe(router, path, func(ctx context.Context) (<-chan any, error) {
			mu.Lock()
			feeds[path]++
			mu.Unlock()
			ch := make(chan any, 1)
			go func() {
				defer func() { mu.Lock(); feeds[path]--; mu.Unlock(); close(ch) }()
				if path != "video.activeDownloadsLive" {
					<-ctx.Done()
					return
				}
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					select {
					case ch <- []any{}:
						mu.Lock()
						samples++
						mu.Unlock()
					case <-ctx.Done():
						return
					}
					select {
					case <-ticker.C:
					case <-ctx.Done():
						return
					}
				}
			}()
			return ch, nil
		})
	}
	handler := subscriptions.NewHandler(router, nil)
	var handlerMu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/trpc/ws", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if sessionExpired {
			refusedHandshakes++
			mu.Unlock()
			http.Error(w, "expired session", http.StatusUnauthorized)
			return
		}
		connections++
		opened++
		mu.Unlock()
		defer func() { mu.Lock(); connections--; mu.Unlock() }()
		handlerMu.Lock()
		current := handler
		handlerMu.Unlock()
		current.ServeHTTP(w, r)
	})
	mux.HandleFunc("/trpc/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		result := []any{}
		status := http.StatusOK
		for _, path := range strings.Split(strings.TrimPrefix(r.URL.Path, "/trpc/"), ",") {
			var data any
			switch path {
			case "auth.session":
				if sessionExpired {
					expiredSessionChecks++
					status = http.StatusUnauthorized
					result = append(result, map[string]any{"error": map[string]any{
						"message": "UNAUTHORIZED", "code": -32001,
						"data": map[string]any{"code": "UNAUTHORIZED", "httpStatus": http.StatusUnauthorized},
					}})
					continue
				}
				// Let the HTTP route guard succeed, then expire before the first
				// WebSocket handshake. Other queries stay successful so only the
				// global connection failure probe can discover the expired session.
				sessionExpired = expireAfterCheck
				data = map[string]any{"user_id": "u1", "login": "alice", "display_name": "Alice", "role": "viewer"}
			case "storage.status":
				data = map[string]any{"state": state, "checked_at": "2026-09-12T12:00:00Z"}
			case "stream.liveIds":
				data = []any{}
			case "archive.queue":
				data = map[string]any{"queue": []any{}, "failures": []any{}}
			case "video.listPage":
				data = map[string]any{"items": []any{}, "next_cursor": nil}
			}
			result = append(result, map[string]any{"result": map[string]any{"data": data}})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("/test/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connections": connections, "feeds": feeds, "opened": opened, "samples": samples,
			"refused_handshakes": refusedHandshakes, "expired_session_checks": expiredSessionChecks,
		})
	})
	mux.HandleFunc("/test/session", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		expireAfterCheck = r.URL.Query().Get("expire_after_check") == "true"
		sessionExpired = false
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/test/storage", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		state = r.URL.Query().Get("state")
		next := state
		mu.Unlock()
		topic.Publish(map[string]any{"state": next})
		w.WriteHeader(http.StatusNoContent)
	})
	// Replacing the handler simulates a backend restart without restarting HTTP.
	mux.HandleFunc("/test/disconnect", func(w http.ResponseWriter, r *http.Request) {
		handlerMu.Lock()
		_ = handler.Close()
		// Change state during the outage without publishing an event. Reconnecting
		// consumers must reseed their HTTP snapshot to discover it.
		mu.Lock()
		if next := r.URL.Query().Get("state"); next != "" {
			state = next
		}
		mu.Unlock()
		handler = subscriptions.NewHandler(router, nil)
		handlerMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	root := os.Args[1]
	files := http.FileServer(http.Dir(root))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(root, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(root, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Printf("http://%s\n", listener.Addr())
	if err := http.Serve(listener, mux); err != nil {
		panic(err)
	}
}
