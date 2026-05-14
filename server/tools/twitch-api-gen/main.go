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
	var genFixtures bool
	flag.BoolVar(&genFixtures, "gen-fixtures", false, "regenerate testdata/normalize/*.{input,expected}.html pairs from the snapshot and exit")
	var schemaInPath, schemaOutPath string
	flag.StringVar(&schemaInPath, "schema-in", "", "read normalized scraper JSON instead of fetching/parsing docs")
	flag.StringVar(&schemaOutPath, "schema-out", "", "write normalized scraper JSON for review")
	var checkCommitted bool
	flag.BoolVar(&checkCommitted, "check", false, "run pipeline to a tempdir and fail if output differs from committed files in -out (CI gate)")
	var explain bool
	flag.BoolVar(&explain, "explain", false, "structured diff of two generated Go files (usage: -explain OLD NEW); report added/removed types, changed fields and validate tags")
	flag.Parse()

	log := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.TimeOnly,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if genFixtures {
		if err := generateNormalizeFixtures(log); err != nil {
			log.Error("gen-fixtures", "err", err)
			os.Exit(1)
		}
		return
	}

	if explain {
		args := flag.Args()
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: -explain OLD NEW")
			os.Exit(2)
		}
		if err := explainDiff(args[0], args[1], os.Stdout); err != nil {
			log.Error("explain", "err", err)
			os.Exit(1)
		}
		return
	}

	committedOutDir := outDir
	if checkCommitted {
		tmpDir, err := os.MkdirTemp("", "twitch-api-gen-check-*")
		if err != nil {
			log.Error("create tempdir", "err", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmpDir)
		outDir = tmpDir
	}

	schema, err := loadOrParseSchema(ctx, schemaInPath, referenceURL, cachePath, eventSubRefURL, eventSubTypesURL, eventSubRefCache, eventSubTypesCache, endpointsFile, refresh, log)
	if err != nil {
		log.Error("load normalized schema", "err", err)
		os.Exit(1)
	}
	if schemaOutPath != "" {
		if err := writeNormalizedSchema(schemaOutPath, schema); err != nil {
			log.Error("write normalized schema", "err", err)
			os.Exit(1)
		}
		log.Info("wrote normalized schema", "path", schemaOutPath)
	}

	if err := Generate(schema.Endpoints, GenerateOptions{
		OutDir:            outDir,
		SourceURL:         schema.SourceURL,
		EventSubReference: schema.EventSubReference,
		EventSubSubs:      schema.EventSubSubscriptions,
		Log:               log,
	}); err != nil {
		log.Error("generate", "err", err)
		os.Exit(1)
	}
	log.Info("generated", "out", outDir)

	if checkCommitted {
		if err := checkAgainstCommitted(outDir, committedOutDir, log); err != nil {
			log.Error("check", "err", err)
			if errors.Is(err, ErrGeneratedFilesStale) {
				os.Exit(2)
			}
			os.Exit(1)
		}
	}
}

func loadOrParseSchema(
	ctx context.Context,
	schemaInPath, referenceURL, cachePath, eventSubRefURL, eventSubTypesURL, eventSubRefCache, eventSubTypesCache, endpointsFile string,
	refresh bool,
	log *slog.Logger,
) (normalizedSchema, error) {
	if schemaInPath != "" {
		schema, err := readNormalizedSchema(schemaInPath)
		if err != nil {
			return normalizedSchema{}, err
		}
		log.Info("loaded normalized schema", "path", schemaInPath, "endpoints", len(schema.Endpoints), "eventsub_subscriptions", len(schema.EventSubSubscriptions))
		return schema, nil
	}

	filter, err := loadEndpointFilter(endpointsFile)
	if err != nil {
		return normalizedSchema{}, fmt.Errorf("load endpoint filter: %w", err)
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
		return normalizedSchema{}, fmt.Errorf("fetch reference html: %w", err)
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
		return normalizedSchema{}, fmt.Errorf("fetch eventsub reference: %w", err)
	}
	eventSubTypesDoc, err := Fetch(ctx, FetchOptions{
		URL:       eventSubTypesURL,
		CachePath: eventSubTypesCache,
		MaxAge:    24 * time.Hour,
		Refresh:   refresh,
		Log:       log,
	})
	if err != nil {
		return normalizedSchema{}, fmt.Errorf("fetch eventsub subscription types: %w", err)
	}

	eventSubRef, eventSubSubs, err := ParseEventSubReference(eventSubRefDoc, eventSubTypesDoc, log)
	if err != nil {
		return normalizedSchema{}, fmt.Errorf("parse eventsub reference: %w", err)
	}
	log.Info("parsed eventsub",
		"conditions", len(eventSubRef.Conditions),
		"events", len(eventSubRef.Events),
		"named_schemas", len(eventSubRef.NamedSchemas),
		"subscription_types", len(eventSubSubs),
	)

	defs, err := ParseAll(doc, filter, log)
	if err != nil {
		return normalizedSchema{}, fmt.Errorf("parse endpoints: %w", err)
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
	return buildNormalizedSchema(referenceURL, defs, eventSubRef, eventSubSubs), nil
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
