package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"media-gallery/internal/gallery"
	"media-gallery/internal/logger"
	"media-gallery/web"
)

type fixture struct {
	handler http.Handler
	root    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte{0xAB}, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("2025/Trip/photo1.jpg", 2048)
	mk("2025/Trip/video1.mp4", 8192)
	mk("2025/Trip/notes.txt", 32)
	if err := os.MkdirAll(filepath.Join(root, "2025/Trip/Day 1"), 0o755); err != nil {
		t.Fatal(err)
	}

	g, err := gallery.New(root, 2) // small page size to exercise pagination
	if err != nil {
		t.Fatal(err)
	}
	log := logger.New(io.Discard, "error")
	srv, err := New(g, log, web.FS())
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{handler: LogRequests(log, srv), root: root}
}

func (f *fixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func decodeResult(t *testing.T, body []byte) gallery.Result {
	t.Helper()
	var res gallery.Result
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, body)
	}
	return res
}

/* ---------- UI and assets ---------- */

func TestIndexServed(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/", "/gallery/", "/gallery/2025/", "/gallery"} {
		w := f.get(t, path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: content type = %q", path, ct)
		}
		if !strings.Contains(w.Body.String(), "Media Gallery") {
			t.Errorf("%s: unexpected body", path)
		}
	}
}

func TestStaticAssets(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/style.css", "/app.js"} {
		w := f.get(t, path)
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Errorf("%s: status = %d", path, w.Code)
		}
	}
}

func TestUnknownRoute(t *testing.T) {
	f := newFixture(t)
	if w := f.get(t, "/nope"); w.Code != http.StatusNotFound {
		t.Errorf("status = %d", w.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	f := newFixture(t)
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/browse"},
		{http.MethodDelete, "/media/2025/Trip/photo1.jpg"},
		{http.MethodPost, "/"},
	} {
		r := httptest.NewRequest(req.method, req.path, nil)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, want 405", req.method, req.path, w.Code)
		}
	}
}

func TestHealthz(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/healthz")
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Errorf("healthz: %d %q", w.Code, w.Body.String())
	}
}

/* ---------- browse API ---------- */

func TestBrowseRoot(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/api/browse")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	res := decodeResult(t, w.Body.Bytes())
	if res.Path != "/" {
		t.Errorf("path = %q", res.Path)
	}
	if len(res.Breadcrumbs) != 1 || res.Breadcrumbs[0].Name != "Home" {
		t.Errorf("breadcrumbs = %+v", res.Breadcrumbs)
	}
	if len(res.Directories) != 1 || res.Directories[0].Name != "2025" {
		t.Errorf("directories = %+v", res.Directories)
	}
	if len(res.Files) != 0 {
		t.Errorf("files = %+v", res.Files)
	}
}

func TestBrowseSubdirectory(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/api/browse?path=%2F2025%2FTrip")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	res := decodeResult(t, w.Body.Bytes())
	if res.Path != "/2025/Trip" {
		t.Errorf("path = %q", res.Path)
	}
	wantCrumbs := []string{"Home", "2025", "Trip"}
	if len(res.Breadcrumbs) != 3 {
		t.Fatalf("breadcrumbs = %+v", res.Breadcrumbs)
	}
	for i, name := range wantCrumbs {
		if res.Breadcrumbs[i].Name != name {
			t.Errorf("crumb %d = %q, want %q", i, res.Breadcrumbs[i].Name, name)
		}
	}
	if len(res.Directories) != 1 || res.Directories[0].Name != "Day 1" {
		t.Errorf("directories = %+v", res.Directories)
	}
	// notes.txt is unsupported and must not appear.
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v", res.Files)
	}
	byName := map[string]gallery.Entry{}
	for _, e := range res.Files {
		byName[e.Name] = e
	}
	img := byName["photo1.jpg"]
	if img.Type != "image" || img.MimeType != "image/jpeg" || img.Size != 2048 {
		t.Errorf("image entry = %+v", img)
	}
	if img.Modified.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("modified not populated: %+v", img)
	}
	vid := byName["video1.mp4"]
	if vid.Type != "video" || vid.MimeType != "video/mp4" {
		t.Errorf("video entry = %+v", vid)
	}
}

func TestBrowsePaginationAndSort(t *testing.T) {
	f := newFixture(t) // page_size default is 2
	w := f.get(t, "/api/browse?path=%2F2025%2FTrip&sort=name&order=asc")
	res := decodeResult(t, w.Body.Bytes())
	if res.Pagination.TotalFiles != 2 || res.Pagination.TotalPages != 1 || res.Pagination.PageSize != 2 {
		t.Errorf("pagination = %+v", res.Pagination)
	}
	if len(res.Files) != 2 || res.Files[0].Name != "photo1.jpg" || res.Files[1].Name != "video1.mp4" {
		t.Errorf("files = %+v", res.Files)
	}

	// Explicit page_size and ordering.
	w = f.get(t, "/api/browse?path=%2F2025%2FTrip&page_size=1&sort=name&order=desc")
	res = decodeResult(t, w.Body.Bytes())
	if len(res.Files) != 1 || res.Files[0].Name != "video1.mp4" {
		t.Errorf("page 1 desc = %+v", res.Files)
	}
	w = f.get(t, "/api/browse?path=%2F2025%2FTrip&page_size=1&sort=name&order=desc&page=2")
	res = decodeResult(t, w.Body.Bytes())
	if len(res.Files) != 1 || res.Files[0].Name != "photo1.jpg" {
		t.Errorf("page 2 desc = %+v", res.Files)
	}
	// Directories remain visible on every page.
	if len(res.Directories) != 1 {
		t.Errorf("directories must not be paginated: %+v", res.Directories)
	}
}

func TestBrowseInvalidParams(t *testing.T) {
	f := newFixture(t)
	cases := []string{
		"/api/browse?page=abc",
		"/api/browse?page=0",
		"/api/browse?page_size=0",
		"/api/browse?page_size=501",
		"/api/browse?sort=bogus",
		"/api/browse?order=sideways",
	}
	for _, c := range cases {
		w := f.get(t, c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c, w.Code)
		}
	}
}

/* ---------- path security ---------- */

func TestBrowseTraversalRejected(t *testing.T) {
	f := newFixture(t)
	// A "secret" outside the tree that must never be reachable.
	if err := os.WriteFile(filepath.Join(filepath.Dir(f.root), "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"/api/browse?path=/..",
		"/api/browse?path=/../..",
		"/api/browse?path=..",
		"/api/browse?path=%2F..%2F..%2Fetc%2Fpasswd", // URL-encoded traversal in the query value
		"/api/browse?path=/2025/../../",
		"/api/browse?path=/2025/%2e%2e/%2e%2e",
	}
	for _, c := range cases {
		w := f.get(t, c)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403 (body: %s)", c, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), f.root) {
			t.Errorf("%s: response leaks filesystem paths: %s", c, w.Body.String())
		}
	}
}

func TestMediaTraversalRejected(t *testing.T) {
	f := newFixture(t)
	// Place a file next to the media root; it must be unreachable.
	outside := filepath.Join(filepath.Dir(f.root), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"/media/../secret.txt",
		"/media/..%2Fsecret.txt", // encoded traversal inside the path
		"/media/2025/../../secret.txt",
		"/media/%2e%2e/secret.txt",
	}
	for _, c := range cases {
		w := f.get(t, c)
		if w.Code == http.StatusOK {
			t.Errorf("%s: traversal succeeded! body: %s", c, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s: leaked secret content", c)
		}
	}
}

/* ---------- media serving ---------- */

func TestServeImage(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/media/2025/Trip/photo1.jpg")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content type = %q", ct)
	}
	if w.Body.Len() != 2048 {
		t.Errorf("body = %d bytes", w.Body.Len())
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges")
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Errorf("content disposition = %q", cd)
	}
}

func TestServeVideoRangeRequest(t *testing.T) {
	f := newFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/media/2025/Trip/video1.mp4", nil)
	r.Header.Set("Range", "bytes=0-99")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if cr := w.Header().Get("Content-Range"); cr != "bytes 0-99/8192" {
		t.Errorf("content-range = %q", cr)
	}
	if w.Body.Len() != 100 {
		t.Errorf("body = %d bytes, want 100", w.Body.Len())
	}

	// An open-ended range must also work (video seeking).
	r = httptest.NewRequest(http.MethodGet, "/media/2025/Trip/video1.mp4", nil)
	r.Header.Set("Range", "bytes=8000-")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.Len() != 192 {
		t.Errorf("open range: %d, %d bytes", w.Code, w.Body.Len())
	}
}

func TestServeMediaErrors(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		path string
		code int
	}{
		{"/media/2025/Trip/notes.txt", http.StatusNotFound},   // unsupported
		{"/media/2025/Trip/missing.jpg", http.StatusNotFound}, // missing
		{"/media/2025/Trip", http.StatusNotFound},             // directory
		{"/media/2025/Nope/photo1.jpg", http.StatusNotFound},
	}
	for _, c := range cases {
		w := f.get(t, c.path)
		if w.Code != c.code {
			t.Errorf("%s: status = %d, want %d", c.path, w.Code, c.code)
		}
	}
}

func TestMediaRedirect(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/media")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/gallery/" {
		t.Errorf("media redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
}

/* ---------- security headers ---------- */

func TestSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	w := f.get(t, "/")
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing frame deny")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("missing CSP on index")
	}
}
