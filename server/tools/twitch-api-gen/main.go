package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
)

func main() {
	var (
		outDir        string
		cachePath     string
		endpointsFile string
		refresh       bool
		referenceURL  string
	)

	var eventSubRefURL, eventSubTypesURL, eventSubRefCache, eventSubTypesCache string
	flag.StringVar(&outDir, "out", "./internal/twitch/", "output directory for generated files")
	flag.StringVar(&cachePath, "cache", "./tmp/reference.html", "path to cache the fetched HTML")
	flag.StringVar(&endpointsFile, "endpoints", "", "optional path to endpoint filter list (one id per line); overrides built-in list")
	flag.BoolVar(&refresh, "refresh", false, "force re-fetch even if cache is fresh")
	flag.StringVar(&referenceURL, "url", "https://dev.twitch.tv/docs/api/reference/", "reference HTML URL")
	flag.StringVar(&eventSubRefURL, "eventsub-ref-url", "https://dev.twitch.tv/docs/eventsub/eventsub-reference/", "EventSub reference HTML URL")
	flag.StringVar(&eventSubTypesURL, "eventsub-types-url", "https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/", "EventSub subscription types HTML URL")
	flag.StringVar(&eventSubRefCache, "eventsub-ref-cache", "./tmp/eventsub-reference.html", "path to cache the EventSub reference HTML")
	flag.StringVar(&eventSubTypesCache, "eventsub-types-cache", "./tmp/eventsub-subscription-types.html", "path to cache the EventSub subscription types HTML")
	flag.BoolVar(&debugDumpFields, "debug-fields", false, "dump response field trees to stdout")
	var timestampOverride string
	flag.StringVar(&timestampOverride, "timestamp", "", "RFC3339 timestamp for generated header; overrides cache file mtime")
	flag.Parse()

	log := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.TimeOnly,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	filter, err := loadEndpointFilter(endpointsFile)
	if err != nil {
		log.Error("load endpoint filter", "err", err)
		os.Exit(1)
	}
	log.Info("endpoint filter loaded", "count", len(filter))

	doc, err := Fetch(ctx, FetchOptions{
		URL:       referenceURL,
		CachePath: cachePath,
		MaxAge:    24 * time.Hour,
		Refresh:   refresh,
		Log:       log,
	})
	if err != nil {
		log.Error("fetch reference html", "err", err)
		os.Exit(1)
	}
	Normalize(doc, log)
	log.Info("normalized reference html")

	eventSubRefDoc, err := Fetch(ctx, FetchOptions{
		URL:       eventSubRefURL,
		CachePath: eventSubRefCache,
		MaxAge:    24 * time.Hour,
		Refresh:   refresh,
		Log:       log,
	})
	if err != nil {
		log.Error("fetch eventsub reference", "err", err)
		os.Exit(1)
	}
	eventSubTypesDoc, err := Fetch(ctx, FetchOptions{
		URL:       eventSubTypesURL,
		CachePath: eventSubTypesCache,
		MaxAge:    24 * time.Hour,
		Refresh:   refresh,
		Log:       log,
	})
	if err != nil {
		log.Error("fetch eventsub subscription types", "err", err)
		os.Exit(1)
	}

	eventSubRef, eventSubSubs, err := ParseEventSubReference(eventSubRefDoc, eventSubTypesDoc, log)
	if err != nil {
		log.Error("parse eventsub reference", "err", err)
		os.Exit(1)
	}
	log.Info("parsed eventsub",
		"conditions", len(eventSubRef.Conditions),
		"events", len(eventSubRef.Events),
		"named_schemas", len(eventSubRef.NamedSchemas),
		"subscription_types", len(eventSubSubs),
	)

	defs, err := ParseAll(doc, filter, log)
	if err != nil {
		log.Error("parse endpoints", "err", err)
		os.Exit(1)
	}
	for _, ep := range defs {
		log.Info("parsed endpoint",
			"id", ep.ID,
			"method", ep.Method,
			"path", ep.Path,
			"auth", ep.AuthType.String(),
			"scopes", ep.Scopes,
			"query", len(ep.QueryParams),
			"body", len(ep.BodyFields),
			"response", len(ep.Response),
			"codes", len(ep.StatusCodes),
			"deprecated", ep.Deprecated,
		)
		if debugDumpFields {
			dumpFields(ep.Response, 0)
		}
	}

	var ts time.Time
	if timestampOverride != "" {
		t, err := time.Parse(time.RFC3339, timestampOverride)
		if err != nil {
			log.Error("parse timestamp override", "err", err)
			os.Exit(1)
		}
		ts = t.UTC()
	} else if fi, err := os.Stat(cachePath); err == nil {
		ts = fi.ModTime().UTC()
	}
	if err := Generate(defs, GenerateOptions{
		OutDir:            outDir,
		SourceURL:         referenceURL,
		Timestamp:         ts,
		EventSubReference: eventSubRef,
		EventSubSubs:      eventSubSubs,
		Log:               log,
	}); err != nil {
		log.Error("generate", "err", err)
		os.Exit(1)
	}
	log.Info("generated", "out", outDir)
}

// debugDumpFields toggles a prettyprint of the parsed field tree per endpoint.
// Flip via the -debug-fields flag.
var debugDumpFields bool

func dumpFields(fields []FieldSchema, indent int) {
	pad := strings.Repeat("  ", indent)
	for _, f := range fields {
		req := "?"
		if f.Required != nil {
			if *f.Required {
				req = "required"
			} else {
				req = "optional"
			}
		}
		fmt.Printf("%s- %s (%s) [%s]\n", pad, f.Name, f.Type, req)
		if len(f.Children) > 0 {
			dumpFields(f.Children, indent+1)
		}
	}
}

// loadEndpointFilter returns the filter list. If path is empty, returns the built-in list.
// Otherwise reads one id per line; blank lines and lines starting with '#' are ignored.
func loadEndpointFilter(path string) ([]string, error) {
	if path == "" {
		out := make([]string, len(endpoints))
		copy(out, endpoints)
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open endpoints file: %w", err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read endpoints file: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("endpoints file is empty")
	}
	return out, nil
}
