// Command probe runs fixture SQL and HTTP requests inside the application container.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type request struct {
	Exec    []string          `json:"exec"`
	Queries map[string]string `json:"queries"`
	Files   map[string][]byte `json:"files"`
	HTTP    *httpRequest      `json:"http"`
}

type httpRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return err
	}
	if req.HTTP != nil {
		r, err := http.NewRequestWithContext(ctx, req.HTTP.Method, "http://127.0.0.1:8080"+req.HTTP.Path, bytes.NewReader(req.HTTP.Body))
		if err != nil {
			return err
		}
		for k, v := range req.HTTP.Headers {
			r.Header.Set(k, v)
		}
		response, err := (&http.Client{Timeout: 10 * time.Second}).Do(r)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Status  int         `json:"status"`
			Headers http.Header `json:"headers"`
			Body    []byte      `json:"body"`
		}{response.StatusCode, response.Header, body})
	}
	driver, dsn := "sqlite", "file:/app/data/replayvod.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if os.Getenv("DATABASE_DRIVER") == "postgres" {
		driver, dsn = "pgx", "postgres://postgres:upgrade@database:5432/replayvod?sslmode=disable"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if len(req.Exec) > 0 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for i, statement := range req.Exec {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("fixture statement %d: %w", i, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	for name, data := range req.Files {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("invalid fixture path %q", name)
		}
		path := filepath.Join("/app/data", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	result := map[string][]map[string]any{}
	for name, query := range req.Queries {
		rows, err := readRows(ctx, db, query)
		if err != nil {
			return fmt.Errorf("query %s: %w", name, err)
		}
		result[name] = rows
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func readRows(ctx context.Context, db *sql.DB, query string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, column := range columns {
			value := values[i]
			switch v := value.(type) {
			case []byte:
				value = hex.EncodeToString(v)
			case time.Time:
				value = v.UTC().Format(time.RFC3339Nano)
			}
			row[column] = value
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
