package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// These tests pin the search behaviour described in docs/SEARCH_SPEC.md.
// Some of it is a known quirk (see the spec's "Known issues"); the tests
// record it so a change is a deliberate decision, not an accident.

// mkTree creates files (and the folders they need) under a fresh temp dir.
// A path ending in "/" is created as an empty folder.
func mkTree(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if p[len(p)-1] == '/' {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchMatchesNamesCaseInsensitiveSubstring(t *testing.T) {
	root := mkTree(t, "Invoice-2026.pdf", "old_INVOICES/", "notes.txt", "sub/my-invoice.txt")

	entries, truncated := walkSearch(root, "invoice", 8, 200, 10)

	if truncated {
		t.Fatal("small tree should not be truncated")
	}
	want := []string{"Invoice-2026.pdf", "my-invoice.txt", "old_INVOICES"}
	if got := names(entries); !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSearchReportsTypeAndSize(t *testing.T) {
	root := mkTree(t, "report/", "report.txt")

	entries, _ := walkSearch(root, "report", 8, 200, 10)

	byName := map[string]Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if byName["report"].Type != "dir" || byName["report"].Size != nil {
		t.Fatalf("folder entry wrong: %+v", byName["report"])
	}
	if byName["report.txt"].Type != "file" || byName["report.txt"].Size == nil || *byName["report.txt"].Size != 1 {
		t.Fatalf("file entry wrong: %+v", byName["report.txt"])
	}
	if byName["report.txt"].Path != filepath.Join(root, "report.txt") {
		t.Fatalf("path should be absolute: %q", byName["report.txt"].Path)
	}
}

func TestSearchQueryIsLiteralNotAWildcard(t *testing.T) {
	root := mkTree(t, "a.txt", "b.txt")

	if entries, _ := walkSearch(root, "*.txt", 8, 200, 10); len(entries) != 0 {
		t.Fatalf("'*' is matched literally today, got %v", names(entries))
	}
}

// max_depth=N: folders at depth N still have their children checked, but
// nothing below that is walked. So matches up to depth N+1 are reported.
func TestSearchDepthLimit(t *testing.T) {
	root := mkTree(t, "hit0.txt", "d1/hit1.txt", "d1/d2/hit2.txt", "d1/d2/d3/hit3.txt")

	if got := names(first(walkSearch(root, "hit", 0, 200, 10))); !equal(got, []string{"hit0.txt"}) {
		t.Fatalf("max_depth 0: got %v", got)
	}
	if got := names(first(walkSearch(root, "hit", 1, 200, 10))); !equal(got, []string{"hit0.txt", "hit1.txt"}) {
		t.Fatalf("max_depth 1: got %v", got)
	}
	if got := names(first(walkSearch(root, "hit", 2, 200, 10))); !equal(got, []string{"hit0.txt", "hit1.txt", "hit2.txt"}) {
		t.Fatalf("max_depth 2: got %v", got)
	}
}

func first(e []Entry, _ bool) []Entry { return e }

func TestSearchStopsAtMaxResultsAndSaysSo(t *testing.T) {
	root := mkTree(t, "m1", "m2", "m3", "m4", "m5")

	entries, truncated := walkSearch(root, "m", 8, 3, 10)

	if len(entries) != 3 || !truncated {
		t.Fatalf("want 3 results and truncated, got %d, %v", len(entries), truncated)
	}
}

// Quirk: hitting the limit exactly on the last match still says truncated.
func TestSearchExactlyMaxResultsIsReportedTruncated(t *testing.T) {
	root := mkTree(t, "m1", "m2", "m3")

	if entries, truncated := walkSearch(root, "m", 8, 3, 10); len(entries) != 3 || !truncated {
		t.Fatalf("got %d, %v", len(entries), truncated)
	}
}

func TestSearchOutOfTimeReturnsTruncated(t *testing.T) {
	root := mkTree(t, "a/b/c/match.txt")

	entries, truncated := walkSearch(root, "match", 8, 200, -1)

	if len(entries) != 0 || !truncated {
		t.Fatalf("an expired deadline should return nothing, truncated; got %d, %v", len(entries), truncated)
	}
}

// Quirk: an unreadable folder is skipped silently; the result is not marked
// truncated, so "no results" can hide folders that were never searched.
func TestSearchSkipsUnreadableFoldersSilently(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := mkTree(t, "locked/secret-match.txt", "open/visible-match.txt")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	entries, truncated := walkSearch(root, "match", 8, 200, 10)

	if got := names(entries); !equal(got, []string{"visible-match.txt"}) || truncated {
		t.Fatalf("got %v truncated=%v", got, truncated)
	}
}

// Symlinked folders are reported if their name matches, but never walked
// into, so a link loop can't make the search run forever.
func TestSearchDoesNotFollowSymlinkedFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	root := mkTree(t, "real/inside-match.txt")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link-match")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "real", "loop")); err != nil {
		t.Fatal(err)
	}

	entries, truncated := walkSearch(root, "match", 8, 200, 10)

	if got := names(entries); !equal(got, []string{"inside-match.txt", "link-match"}) || truncated {
		t.Fatalf("got %v truncated=%v", got, truncated)
	}
	for _, e := range entries {
		if e.Name == "link-match" && e.Type != "dir" {
			t.Fatalf("a link to a folder is reported as a folder (os.Stat follows it), got %q", e.Type)
		}
	}
}

func searchRequest(t *testing.T, params url.Values) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handleSearch(rec, httptest.NewRequest(http.MethodGet, "/api/search?"+params.Encode(), nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestSearchHandlerRequiresAQuery(t *testing.T) {
	root := mkTree(t, "a.txt")

	rec, body := searchRequest(t, url.Values{"path": {root}})

	if rec.Code != 400 || body["message"] != "query is required." {
		t.Fatalf("got %d %v", rec.Code, body)
	}
}

func TestSearchHandlerRejectsAPathThatIsNotAFolder(t *testing.T) {
	root := mkTree(t, "a.txt")

	if rec, _ := searchRequest(t, url.Values{"path": {filepath.Join(root, "a.txt")}, "query": {"a"}}); rec.Code != 404 {
		t.Fatalf("file path: got %d", rec.Code)
	}
	if rec, _ := searchRequest(t, url.Values{"path": {filepath.Join(root, "missing")}, "query": {"a"}}); rec.Code != 404 {
		t.Fatalf("missing path: got %d", rec.Code)
	}
}

func TestSearchHandlerResponseShapeAndParams(t *testing.T) {
	root := mkTree(t, "m1", "m2", "m3", "d/m4")

	rec, body := searchRequest(t, url.Values{"path": {root}, "query": {"m"}, "max_results": {"2"}})

	if rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
	if body["path"] != root || body["query"] != "m" || body["truncated"] != true {
		t.Fatalf("unexpected body %v", body)
	}
	if n := len(body["entries"].([]any)); n != 2 {
		t.Fatalf("max_results ignored: %d entries", n)
	}

	// Unparseable numbers fall back to the defaults instead of failing.
	rec, body = searchRequest(t, url.Values{"path": {root}, "query": {"m"}, "max_results": {"lots"}, "max_depth": {"x"}, "timeout": {"soon"}})
	if rec.Code != 200 || len(body["entries"].([]any)) != 4 || body["truncated"] != false {
		t.Fatalf("defaults not applied: %d %v", rec.Code, body)
	}
}

func TestSearchHandlerHonoursAllowedRoots(t *testing.T) {
	allowed := mkTree(t, "in-match.txt")
	outside := mkTree(t, "out-match.txt")
	saved := allowedRoots
	allowedRoots = nil
	setAllowedRoots([]string{allowed})
	t.Cleanup(func() { allowedRoots = saved })

	if rec, _ := searchRequest(t, url.Values{"path": {outside}, "query": {"match"}}); rec.Code != 403 {
		t.Fatalf("outside allowed roots: got %d", rec.Code)
	}
	if rec, body := searchRequest(t, url.Values{"path": {allowed}, "query": {"match"}}); rec.Code != 200 || len(body["entries"].([]any)) != 1 {
		t.Fatalf("inside allowed roots: got %d %v", rec.Code, body)
	}
}
