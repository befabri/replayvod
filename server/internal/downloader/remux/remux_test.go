package remux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func TestPrepareInput_TS_NumericSortNotLex(t *testing.T) {
	// Numeric sort regression: lexicographic order would put
	// "10.ts" before "2.ts" and produce a garbled output.
	// Write the files out-of-order to confirm the scan step
	// does the numeric parse.
	dir := t.TempDir()
	for _, name := range []string{"10.ts", "2.ts", "0.ts", "1.ts", "100.ts"} {
		writeFile(t, filepath.Join(dir, name), "x")
	}

	inputPath, err := PrepareInput(dir, ModeTS)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	body, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("line count=%d, want 5: %q", len(lines), body)
	}
	want := []string{"0.ts", "1.ts", "2.ts", "10.ts", "100.ts"}
	for i, line := range lines {
		// line format: file '/abs/path/<seq>.ts'
		if !strings.HasSuffix(line, "/"+want[i]+"'") {
			t.Errorf("line %d=%q, want suffix /%s'", i, line, want[i])
		}
	}
}

func TestPrepareInput_TS_IgnoresNonNumericFiles(t *testing.T) {
	// init.mp4 / media.m3u8 / segments.txt / stray .part files
	// shouldn't slip into the concat input.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0.ts"), "x")
	writeFile(t, filepath.Join(dir, "1.ts"), "x")
	writeFile(t, filepath.Join(dir, "init.mp4"), "init")
	writeFile(t, filepath.Join(dir, "2.ts.part"), "partial") // crashed fetch
	writeFile(t, filepath.Join(dir, "README"), "hi")

	inputPath, err := PrepareInput(dir, ModeTS)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	body, _ := os.ReadFile(inputPath)
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count=%d, want 2. Body=%q", len(lines), body)
	}
}

func TestPrepareInput_TS_EmptyDirFails(t *testing.T) {
	dir := t.TempDir()
	_, err := PrepareInput(dir, ModeTS)
	if err == nil {
		t.Fatal("expected error on empty dir")
	}
	if !strings.Contains(err.Error(), "no .ts") {
		t.Errorf("err=%v, want mention of no .ts", err)
	}
}

func TestPrepareInput_FMP4_WritesPlaylistWithMap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "init.mp4"), "init")
	writeFile(t, filepath.Join(dir, "10.m4s"), "x")
	writeFile(t, filepath.Join(dir, "2.m4s"), "x")
	writeFile(t, filepath.Join(dir, "1.m4s"), "x")

	inputPath, err := PrepareInput(dir, ModeFMP4)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if filepath.Base(inputPath) != "media.m3u8" {
		t.Errorf("inputPath base=%s, want media.m3u8", filepath.Base(inputPath))
	}
	body, _ := os.ReadFile(inputPath)
	text := string(body)
	// EXT-X-MAP must reference the absolute init path so ffmpeg
	// resolves it regardless of its cwd.
	absInit, _ := filepath.Abs(filepath.Join(dir, "init.mp4"))
	if !strings.Contains(text, `#EXT-X-MAP:URI="`+absInit+`"`) {
		t.Errorf("missing absolute EXT-X-MAP for init: %s", text)
	}
	if !strings.Contains(text, "#EXT-X-ENDLIST") {
		t.Error("missing EXT-X-ENDLIST")
	}
	// Segments numeric-sorted.
	i1 := strings.Index(text, "1.m4s")
	i2 := strings.Index(text, "2.m4s")
	i10 := strings.Index(text, "10.m4s")
	if !(i1 < i2 && i2 < i10) {
		t.Errorf("segment order wrong: 1=%d 2=%d 10=%d", i1, i2, i10)
	}
}

func TestPrepareInput_FMP4_MissingInitFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0.m4s"), "x")
	_, err := PrepareInput(dir, ModeFMP4)
	if err == nil {
		t.Fatal("expected error when init.mp4 missing")
	}
	if !strings.Contains(err.Error(), "init.mp4 missing") {
		t.Errorf("err=%v, want init.mp4 missing", err)
	}
}

func TestPrepareInput_UnknownMode(t *testing.T) {
	dir := t.TempDir()
	_, err := PrepareInput(dir, Mode("weird"))
	if err == nil {
		t.Fatal("expected unknown-mode error")
	}
}

func TestPrepareInput_NonExistentDir(t *testing.T) {
	_, err := PrepareInput("/this/path/does/not/exist/anywhere", ModeTS)
	if err == nil {
		t.Fatal("expected error on missing dir")
	}
}

func TestPrepareInput_TargetIsFileNotDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	writeFile(t, file, "x")
	_, err := PrepareInput(file, ModeTS)
	if err == nil {
		t.Fatal("expected not-a-directory error")
	}
}

func TestPrepareInput_Idempotent(t *testing.T) {
	// Running PrepareInput twice on the same dir overwrites
	// cleanly and produces the same output bytes.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "0.ts"), "x")
	writeFile(t, filepath.Join(dir, "1.ts"), "x")

	first, err := PrepareInput(dir, ModeTS)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	firstBody, _ := os.ReadFile(first)

	second, err := PrepareInput(dir, ModeTS)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	secondBody, _ := os.ReadFile(second)

	if string(firstBody) != string(secondBody) {
		t.Errorf("second prep differs:\nfirst:\n%s\nsecond:\n%s", firstBody, secondBody)
	}
}

func TestKind_OutputExt(t *testing.T) {
	if got := KindVideo.OutputExt(); got != ".mp4" {
		t.Errorf("video ext=%s", got)
	}
	if got := KindAudio.OutputExt(); got != ".m4a" {
		t.Errorf("audio ext=%s", got)
	}
	// Unknown kind falls back to .mp4 rather than producing an
	// extensionless path (which would break MIME sniffing).
	if got := Kind("weird").OutputExt(); got != ".mp4" {
		t.Errorf("unknown ext=%s, want .mp4", got)
	}
}
