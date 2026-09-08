package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalProbeRootRejectsReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	if err := os.MkdirAll(filepath.Join(root, "videos"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ProbeRoot(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "videos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.ProbeRoot(t.Context()); err == nil {
		t.Fatal("accepted replacement root with videos directory")
	}
}
func TestLocalProbeRootHonorsCancellation(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.ProbeRoot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe = %v", err)
	}
}

func TestS3ProbeRootFailsClosed(t *testing.T) {
	for _, status := range []int{200, 403, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead || r.URL.Path != "/recordings" {
					t.Errorf("unexpected probe %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer srv.Close()
			client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(srv.URL), UsePathStyle: true, RetryMaxAttempts: 1, Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")})
			store := &S3Storage{client: client, bucket: "recordings"}
			err := store.ProbeRoot(t.Context())
			if (err == nil) != (status == 200) {
				t.Fatalf("status %d: probe = %v", status, err)
			}
		})
	}
}
