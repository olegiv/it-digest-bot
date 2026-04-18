package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSmokePostDryRun builds the binary and runs `digest post --dry-run`
// against the checked-in config.example.toml. Its job is to catch
// regressions in the end-to-end rendering path — config parsing, logger
// init, MarkdownV2 formatter — without touching the network or the DB.
func TestSmokePostDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("smoke test skipped in -short mode")
	}

	repoRoot := findRepoRoot(t)

	cmd := exec.Command("go", "run", "./cmd/digest",
		"post", "--dry-run",
		"--config", filepath.Join(repoRoot, "config.example.toml"))
	cmd.Dir = repoRoot
	cmd.Env = append(cmd.Environ(), "TELEGRAM_BOT_TOKEN=smoke-stub")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("digest post --dry-run failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{
		"BEGIN DRY-RUN MESSAGE",
		"END DRY-RUN MESSAGE",
		"🚀 *Claude Code*",
		"`2.1.114`",
		"[npm](",
		"[GitHub](",
		`\#ClaudeCode`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered message missing %q\nfull output:\n%s", want, out)
		}
	}
}

// findRepoRoot walks up from this test file's directory until it finds
// go.mod, returning that directory.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + filepath.Dir(self))
		}
		dir = parent
	}
}
