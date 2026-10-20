package gallery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileSpec is an ordered file description; earlier entries get earlier
// modification times so date sorting is deterministic.
type fileSpec struct {
	name string
	size int64
}

// testGallery builds a media tree from an ordered list of files and a list
// of directories.
func testGallery(t *testing.T, files []fileSpec, dirs []string) (*Gallery, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for i, f := range files {
		p := filepath.Join(root, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, f.size), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	g, err := New(root, 50)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g, root
}

func mustBrowse(t *testing.T, g *Gallery, path string, opts Options) *Result {
	t.Helper()
	res, err := g.Browse(path, opts)
	if err != nil {
		t.Fatalf("Browse(%q): %v", path, err)
	}
	return res
}

func fileNames(res *Result) []string {
	names := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		names = append(names, f.Name)
	}
	return names
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		in    string
		want  string
		error bool
	}{
		{"", "/", false},
		{"/", "/", false},
		{"2025", "/2025", false},
		{"/2025", "/2025", false},
		{"/2025/", "/2025", false},
		{"/2025/Trip", "/2025/Trip", false},
		{"/..", "", true},
		{"/../etc/passwd", "", true},
		{"/2025/../../etc", "", true},
		{"..", "", true},
		{".", "", true},
		{"/a/./b", "", true},
		{"/a//b", "", true},
		{"\\", "", true},
		{"/a\\b", "", true},
		{"a\x00b", "", true},
		{"/a\x01b", "", true},
		{"/a/%2e", "/a/%2e", false}, // already decoded once upstream; literal name is safe
		{"/ spaced name ", "/ spaced name ", false},
		{"/café", "/café", false},
	}
	for _, tt := range tests {
		got, err := SanitizePath(tt.in)
		if tt.error {
			if err == nil {
				t.Errorf("SanitizePath(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizePath(%q): unexpected error %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("SanitizePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBrowseEmptyDir(t *testing.T) {
	g, _ := testGallery(t, nil, nil)
	res := mustBrowse(t, g, "/", Options{Sort: SortDate, Order: OrderDesc, Page: 1, PageSize: 50})
	if len(res.Directories) != 0 || len(res.Files) != 0 {
		t.Errorf("expected empty, got %+v", res)
	}
	if res.Pagination.TotalFiles != 0 || res.Pagination.TotalPages != 0 {
		t.Errorf("pagination: %+v", res.Pagination)
	}
	if len(res.Breadcrumbs) != 1 || res.Breadcrumbs[0].Name != "Home" {
		t.Errorf("breadcrumbs: %+v", res.Breadcrumbs)
	}
}

func TestBrowseOnlyDirs(t *testing.T) {
	g, _ := testGallery(t, nil, []string{"2024", "2025"})
	res := mustBrowse(t, g, "/", Options{Sort: SortDate, Order: OrderDesc, Page: 1, PageSize: 50})
	if len(res.Directories) != 2 || len(res.Files) != 0 {
		t.Fatalf("got %+v", res)
	}
	// Directories follow the requested order (desc here).
	if res.Directories[0].Name != "2025" || res.Directories[0].Path != "/2025" {
		t.Errorf("dirs: %+v", res.Directories)
	}
	res = mustBrowse(t, g, "/", Options{Sort: SortDate, Order: OrderAsc, Page: 1, PageSize: 50})
	if res.Directories[0].Name != "2024" {
		t.Errorf("dirs asc: %+v", res.Directories)
	}
}

func TestBrowseMixed(t *testing.T) {
	files := []fileSpec{
		{"2025/Trip/photo1.jpg", 100},
		{"2025/Trip/photo2.png", 200},
		{"2025/Trip/video1.mp4", 300},
		{"2025/Trip/photo3.webp", 400},
		{"2025/Trip/photo4.avif", 500},
		{"2025/Trip/holiday.webm", 600},
		{"2025/Trip/notes.txt", 10}, // unsupported -> ignored
		{"2025/Trip/data.csv", 10},  // unsupported -> ignored
	}
	dirs := []string{"2025/Trip/Day 1", "2025/Trip/Day 2", "2025/Family"}
	g, _ := testGallery(t, files, dirs)
	res := mustBrowse(t, g, "/2025/Trip", Options{Sort: SortName, Order: OrderAsc, Page: 1, PageSize: 50})

	if len(res.Directories) != 2 {
		t.Errorf("dirs: %+v", res.Directories)
	}
	if res.Directories[0].Name != "Day 1" || res.Directories[1].Name != "Day 2" {
		t.Errorf("dir order: %+v", res.Directories)
	}
	if len(res.Files) != 6 {
		t.Fatalf("files: %+v", res.Files)
	}
	for _, f := range res.Files {
		if f.Type == "" || f.MimeType == "" || f.Size == 0 {
			t.Errorf("incomplete entry: %+v", f)
		}
		if !strings.HasPrefix(f.Path, "/2025/Trip/") {
			t.Errorf("entry path: %+v", f)
		}
	}
	if res.Files[0].Type != MediaTypeVideo || res.Files[0].MimeType != "video/webm" {
		t.Errorf("webm entry: %+v", res.Files[0])
	}
	if res.Pagination.TotalFiles != 6 || res.Pagination.TotalPages != 1 {
		t.Errorf("pagination: %+v", res.Pagination)
	}
}

func TestBrowseHiddenEntries(t *testing.T) {
	g, _ := testGallery(t, []fileSpec{{".secret.jpg", 10}, {"visible.jpg", 10}}, []string{".hiddendir"})
	res := mustBrowse(t, g, "/", Options{Sort: SortName, Order: OrderAsc, Page: 1, PageSize: 50})
	if len(res.Directories) != 0 || len(res.Files) != 1 || res.Files[0].Name != "visible.jpg" {
		t.Errorf("hidden entries leaked: %+v", res)
	}
}

func TestBrowseSymlinks(t *testing.T) {
	g, root := testGallery(t, []fileSpec{{"real.jpg", 10}}, nil)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.jpg")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escapeDir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.jpg"), filepath.Join(root, "inside.jpg")); err != nil {
		t.Fatal(err)
	}

	// Symlinks are skipped in listings.
	res := mustBrowse(t, g, "/", Options{Sort: SortName, Order: OrderAsc, Page: 1, PageSize: 50})
	if len(res.Files) != 1 || res.Files[0].Name != "real.jpg" {
		t.Errorf("symlinks leaked into listing: %+v", res.Files)
	}

	// A symlink pointing outside the root cannot be opened.
	if _, _, err := g.OpenMedia("/escape.jpg"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("OpenMedia(/escape.jpg) err = %v, want ErrInvalidPath", err)
	}
	// A symlink pointing outside cannot be browsed through.
	if _, err := g.Browse("/escapeDir", Options{Sort: SortDate, Order: OrderDesc, Page: 1, PageSize: 50}); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Browse(/escapeDir) err = %v, want ErrInvalidPath", err)
	}
}

func TestBrowsePathErrors(t *testing.T) {
	g, _ := testGallery(t, []fileSpec{{"a.jpg", 1}}, []string{"sub"})
	cases := []struct {
		path string
		want error
	}{
		{"/..", ErrInvalidPath},
		{"/../..", ErrInvalidPath},
		{"/nope", ErrNotFound},
		{"/a.jpg/x", ErrNotFound}, // not a directory
	}
	for _, c := range cases {
		_, err := g.Browse(c.path, Options{Sort: SortDate, Order: OrderDesc, Page: 1, PageSize: 50})
		if !errors.Is(err, c.want) {
			t.Errorf("Browse(%q) err = %v, want %v", c.path, err, c.want)
		}
	}
}

func TestBrowseBadOptions(t *testing.T) {
	g, _ := testGallery(t, nil, nil)
	bad := []Options{
		{Sort: "bogus", Order: "asc", Page: 1, PageSize: 10},
		{Sort: "name", Order: "sideways", Page: 1, PageSize: 10},
		{Sort: "name", Order: "asc", Page: 0, PageSize: 10},
		{Sort: "name", Order: "asc", Page: 1, PageSize: 0},
		{Sort: "name", Order: "asc", Page: 1, PageSize: 501},
	}
	for i, o := range bad {
		if _, err := g.Browse("/", o); !errors.Is(err, ErrBadOptions) {
			t.Errorf("case %d: err = %v, want ErrBadOptions", i, err)
		}
	}
}

func TestSorting(t *testing.T) {
	// Modification times increase in list order:
	// bravo < alpha < charlie < ALPHA2.
	files := []fileSpec{
		{"bravo.jpg", 30},
		{"alpha.jpg", 100},
		{"charlie.jpg", 10},
		{"ALPHA2.jpg", 50},
	}
	g, _ := testGallery(t, files, nil)

	tests := []struct {
		sort, order string
		want        []string
	}{
		{SortName, OrderAsc, []string{"alpha.jpg", "ALPHA2.jpg", "bravo.jpg", "charlie.jpg"}},
		{SortName, OrderDesc, []string{"charlie.jpg", "bravo.jpg", "ALPHA2.jpg", "alpha.jpg"}},
		{SortSize, OrderAsc, []string{"charlie.jpg", "bravo.jpg", "ALPHA2.jpg", "alpha.jpg"}},
		{SortSize, OrderDesc, []string{"alpha.jpg", "ALPHA2.jpg", "bravo.jpg", "charlie.jpg"}},
		{SortDate, OrderAsc, []string{"bravo.jpg", "alpha.jpg", "charlie.jpg", "ALPHA2.jpg"}},
		{SortDate, OrderDesc, []string{"ALPHA2.jpg", "charlie.jpg", "alpha.jpg", "bravo.jpg"}},
	}
	for _, tt := range tests {
		res := mustBrowse(t, g, "/", Options{Sort: tt.sort, Order: tt.order, Page: 1, PageSize: 50})
		got := fileNames(res)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("sort=%s order=%s got %v want %v", tt.sort, tt.order, got, tt.want)
		}
	}
}

func TestDefaultOptions(t *testing.T) {
	files := []fileSpec{{"a.jpg", 1}, {"b.jpg", 2}}
	g, _ := testGallery(t, files, nil)
	res, err := g.Browse("/", Options{Page: 1, PageSize: 50}) // sort/order empty
	if err != nil {
		t.Fatal(err)
	}
	// Default sort is date descending; b.jpg is newer than a.jpg.
	if res.Files[0].Name != "b.jpg" || res.Files[1].Name != "a.jpg" {
		t.Errorf("default order: %+v", fileNames(res))
	}
}

// fmtName builds deterministic "f000.jpg" style names.
func fmtName(i int) string {
	return fmt.Sprintf("f%03d.jpg", i)
}

func TestPaginationPages(t *testing.T) {
	files := make([]fileSpec, 0, 120)
	for i := 0; i < 120; i++ {
		files = append(files, fileSpec{fmtName(i), int64(i)})
	}
	g, _ := testGallery(t, files, nil)
	opts := func(page, size int) Options {
		return Options{Sort: SortName, Order: OrderAsc, Page: page, PageSize: size}
	}

	res := mustBrowse(t, g, "/", opts(1, 50))
	if len(res.Files) != 50 || res.Files[0].Name != "f000.jpg" || res.Files[49].Name != "f049.jpg" {
		t.Errorf("first page: %d files, first=%s", len(res.Files), res.Files[0].Name)
	}
	if res.Pagination.TotalPages != 3 || res.Pagination.TotalFiles != 120 {
		t.Errorf("pagination meta: %+v", res.Pagination)
	}

	res = mustBrowse(t, g, "/", opts(2, 50))
	if len(res.Files) != 50 || res.Files[0].Name != "f050.jpg" {
		t.Errorf("middle page: first=%s", res.Files[0].Name)
	}

	res = mustBrowse(t, g, "/", opts(3, 50))
	if len(res.Files) != 20 || res.Files[0].Name != "f100.jpg" {
		t.Errorf("last page: %d files, first=%s", len(res.Files), res.Files[0].Name)
	}

	// Page beyond available results: empty file list, still valid response.
	res = mustBrowse(t, g, "/", opts(4, 50))
	if len(res.Files) != 0 || res.Pagination.TotalPages != 3 {
		t.Errorf("beyond page: %+v", res.Pagination)
	}

	// Different page size.
	res = mustBrowse(t, g, "/", opts(2, 10))
	if len(res.Files) != 10 || res.Files[0].Name != "f010.jpg" {
		t.Errorf("page size 10: first=%s", res.Files[0].Name)
	}

	// Directories are never paginated away.
	g2, _ := testGallery(t, files, []string{"zDir"})
	res = mustBrowse(t, g2, "/", opts(3, 50))
	if len(res.Directories) != 1 {
		t.Errorf("directories must not be paginated: %+v", res.Directories)
	}
	if len(res.Files) != 20 {
		t.Errorf("files on last page: %d", len(res.Files))
	}
}

func TestOpenMedia(t *testing.T) {
	files := []fileSpec{
		{"photo.jpg", 4},
		{"clip.mp4", 8},
		{"movie.m4v", 8},
		{"anim.webm", 8},
		{"notes.txt", 4},
		{".hidden.jpg", 4},
	}
	g, root := testGallery(t, files, nil)

	cases := []struct {
		path string
		want error
	}{
		{"/photo.jpg", nil},
		{"/clip.mp4", nil},
		{"/movie.m4v", nil},
		{"/anim.webm", nil},
		{"/notes.txt", ErrUnsupported},
		{"/missing.jpg", ErrNotFound},
		{"/.hidden.jpg", ErrNotFound},
		{"/../etc/passwd", ErrInvalidPath},
		{"/photo.jpg/", nil}, // trailing slash tolerated and normalized
	}
	for _, c := range cases {
		f, fi, err := g.OpenMedia(c.path)
		if c.want != nil {
			if !errors.Is(err, c.want) {
				t.Errorf("OpenMedia(%q) err = %v, want %v", c.path, err, c.want)
			}
			continue
		}
		if err != nil {
			t.Errorf("OpenMedia(%q): %v", c.path, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("OpenMedia(%q): zero size", c.path)
		}
		// Content is readable from the real file.
		buf := make([]byte, 4)
		if _, err := f.Read(buf); err != nil {
			t.Errorf("OpenMedia(%q): read: %v", c.path, err)
		}
		f.Close()
	}

	// Opening a directory as media fails.
	if _, _, err := g.OpenMedia("/"); err == nil {
		t.Error("opening root as media should fail")
	}

	if g.Root() != root {
		t.Errorf("root = %q, want %q", g.Root(), root)
	}
}

func TestMimeType(t *testing.T) {
	g, _ := testGallery(t, nil, nil)
	if got := g.MimeType("x.PNG"); got != "image/png" {
		t.Errorf("PNG mime = %q", got)
	}
	if got := g.MimeType("x.AVIF"); got != "image/avif" {
		t.Errorf("AVIF mime = %q", got)
	}
	if got := g.MimeType("x.MP4"); got != "video/mp4" {
		t.Errorf("MP4 mime = %q", got)
	}
	if got := g.MimeType("x.exe"); got != "" {
		t.Errorf("exe mime = %q", got)
	}
}

func TestBreadcrumbs(t *testing.T) {
	crumbs := breadcrumbs("/2025/Trip")
	want := []Breadcrumb{
		{"Home", "/"},
		{"2025", "/2025"},
		{"Trip", "/2025/Trip"},
	}
	if len(crumbs) != len(want) {
		t.Fatalf("got %+v want %+v", crumbs, want)
	}
	for i := range want {
		if crumbs[i] != want[i] {
			t.Errorf("crumb %d: %+v want %+v", i, crumbs[i], want[i])
		}
	}
}

func TestNewRejectsBadPageSize(t *testing.T) {
	if _, err := New(t.TempDir(), 0); err == nil {
		t.Error("expected error for page size 0")
	}
}

func TestNewRejectsMissingRoot(t *testing.T) {
	if _, err := New("/definitely/not/here", 50); err == nil {
		t.Error("expected error for missing root")
	}
}

func TestReadonlyRoot(t *testing.T) {
	// The application never writes; verify browsing works on a
	// read-only directory tree.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "p.jpg"), make([]byte, 16), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	g, err := New(root, 50)
	if err != nil {
		t.Fatal(err)
	}
	res := mustBrowse(t, g, "/", Options{Sort: SortName, Order: OrderAsc, Page: 1, PageSize: 50})
	if len(res.Files) != 1 {
		t.Errorf("readonly browse failed: %+v", res)
	}
}
