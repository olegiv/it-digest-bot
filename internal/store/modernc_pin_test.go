package store

import (
	"os/exec"
	"strings"
	"testing"
)

// TestModerncPinnedToSQLite guards the "fragile modernc.org/libc
// dependency" rule of modernc.org/sqlite: the module must use exactly the
// modernc.org/libc (and its allocator, modernc.org/memory) versions that the
// selected sqlite release requires in its own go.mod. A plain
// `go get -u ./...` raises them independently; fix that with
// `go get modernc.org/libc@<version> modernc.org/memory@<version>`.
// See https://gitlab.com/cznic/sqlite/-/issues/177.
func TestModerncPinnedToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go tool")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not in PATH")
	}
	selected := map[string]string{}
	out, err := exec.Command(goBin, "list", "-m", "-f", "{{.Path}} {{.Version}}",
		"modernc.org/sqlite", "modernc.org/libc", "modernc.org/memory").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	for line := range strings.Lines(string(out)) {
		if path, version, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			selected[path] = version
		}
	}
	sqlite := "modernc.org/sqlite@" + selected["modernc.org/sqlite"]

	graph, err := exec.Command(goBin, "mod", "graph").Output()
	if err != nil {
		t.Fatalf("go mod graph: %v", err)
	}
	checked := 0
	for line := range strings.Lines(string(graph)) {
		from, to, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || from != sqlite {
			continue
		}
		path, want, _ := strings.Cut(to, "@")
		if path != "modernc.org/libc" && path != "modernc.org/memory" {
			continue
		}
		checked++
		if got := selected[path]; got != want {
			t.Errorf("%s requires %s %s, but go.mod selects %s: run go get %s@%s", sqlite, path, want, got, path, want)
		}
	}
	if checked != 2 {
		t.Errorf("found %d of 2 modernc requirements of %s in go mod graph", checked, sqlite)
	}
}
