// Package gallery implements read-only browsing of the configured media
// directory: safe path resolution, per-request directory scanning, sorting,
// pagination and opening of supported media files.
//
// Design notes:
//   - The directory is scanned on every request. There is no database, no
//     index, no watcher and no thumbnail generation. Only the directory
//     actually being viewed is read.
//   - Media type is derived from the file extension only. Unsupported files
//     are ignored (they do not appear in listings and cannot be served).
//   - Paths arriving from the network are "virtual paths" (e.g. "/2025/Trip").
//     They are strictly validated and resolved against the media root, with
//     symlink resolution checked so that nothing can escape the root.
//   - Entries whose names start with "." are hidden from listings and cannot
//     be opened. Symlinks are skipped in listings and are never followed
//     outside the root.
package gallery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Sentinel errors. The server maps these to HTTP status codes and always
// sends a generic message; detailed errors only go to the log.
var (
	ErrInvalidPath = errors.New("invalid path")          // -> 403
	ErrBadOptions  = errors.New("invalid query options") // -> 400
	ErrNotFound    = errors.New("not found")             // -> 404
	ErrNoAccess    = errors.New("access denied")         // -> 403
	ErrUnsupported = errors.New("unsupported media")     // -> 404 (treated as absent)
	ErrInternal    = errors.New("internal error")        // -> 500
)

// Sort keys.
const (
	SortName = "name"
	SortDate = "date"
	SortSize = "size"
)

// Sort directions.
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// Media types.
const (
	MediaTypeImage = "image"
	MediaTypeVideo = "video"
)

// maxPageSize caps page_size in API requests.
const maxPageSize = 500

// supportedMedia is the single source of truth for supported formats.
// Only formats that modern browsers can display natively are listed;
// anything else is ignored, never converted.
var supportedMedia = []struct {
	ext      string
	mimeType string
	kind     string
}{
	{".jpg", "image/jpeg", MediaTypeImage},
	{".jpeg", "image/jpeg", MediaTypeImage},
	{".png", "image/png", MediaTypeImage},
	{".gif", "image/gif", MediaTypeImage},
	{".webp", "image/webp", MediaTypeImage},
	{".avif", "image/avif", MediaTypeImage},
	{".mp4", "video/mp4", MediaTypeVideo},
	{".m4v", "video/mp4", MediaTypeVideo},
	{".webm", "video/webm", MediaTypeVideo},
}

type mediaInfo struct {
	mimeType string
	kind     string
}

// Gallery is a read-only view of the media root.
type Gallery struct {
	root     string // symlink-resolved absolute media root
	pageSize int    // default page size
	media    map[string]mediaInfo
}

// New opens the media root. The path is resolved to its real location so
// that later containment checks are exact. Callers should have validated
// that root exists and is a directory (config.Validate does this).
func New(root string, pageSize int) (*Gallery, error) {
	if pageSize < 1 {
		return nil, fmt.Errorf("gallery: page size must be at least 1 (got %d)", pageSize)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("gallery: resolve media root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("gallery: media root %q does not exist", root)
		}
		return nil, fmt.Errorf("gallery: media root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("gallery: media root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("gallery: media root %q is not a directory", root)
	}
	media := make(map[string]mediaInfo, len(supportedMedia))
	for _, m := range supportedMedia {
		media[m.ext] = mediaInfo{mimeType: m.mimeType, kind: m.kind}
	}
	return &Gallery{root: resolved, pageSize: pageSize, media: media}, nil
}

// Root returns the resolved media root path (for startup logging only).
func (g *Gallery) Root() string { return g.root }

// PageSize returns the configured default page size.
func (g *Gallery) PageSize() int { return g.pageSize }

// classify returns the media info for a filename, or false if the
// extension is not a supported browser-friendly format.
func classify(media map[string]mediaInfo, name string) (mediaInfo, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	mi, ok := media[ext]
	return mi, ok
}

// SanitizePath validates a user-supplied virtual path and returns its
// canonical form ("/" for the root, otherwise "/a/b" with no trailing
// slash). It rejects anything that could be an escape or smuggling attempt:
// "." and ".." segments, empty segments, backslashes, NUL and control
// characters. Percent-decoding has already happened once at the HTTP layer;
// this function deliberately never decodes again.
func SanitizePath(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", ErrInvalidPath
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p == "/" {
		return "/", nil
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/", nil
	}
	segs := strings.Split(p[1:], "/")
	for _, seg := range segs {
		switch seg {
		case "", ".", "..":
			return "", ErrInvalidPath
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return "", ErrInvalidPath
			}
		}
	}
	return "/" + strings.Join(segs, "/"), nil
}

// resolve maps a validated virtual path to a real filesystem path, proving
// that the fully symlink-resolved result stays inside the media root.
func (g *Gallery) resolve(vpath string) (string, error) {
	clean, err := SanitizePath(vpath)
	if err != nil {
		return "", err
	}
	if clean == "/" {
		return g.root, nil
	}
	candidate := filepath.Join(g.root, clean[1:]) // clean has no "..", so Join cannot escape
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return "", ErrNotFound
		}
		if errors.Is(err, fs.ErrPermission) {
			return "", ErrNoAccess
		}
		return "", fmt.Errorf("gallery: resolve %q: %w", clean, err)
	}
	rel, err := filepath.Rel(g.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return resolved, nil
}

// Entry is one supported media file.
type Entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"` // virtual path, e.g. "/2025/Trip/IMG_1234.jpg"
	Type     string    `json:"type"` // "image" or "video"
	MimeType string    `json:"mime_type"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Directory is a subdirectory of the browsed directory.
type Directory struct {
	Name string `json:"name"`
	Path string `json:"path"` // virtual path, e.g. "/2025/Trip/Day 1"
}

// Breadcrumb is one element of the navigation trail.
type Breadcrumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Pagination describes the file pagination of a browse result.
type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalFiles int `json:"total_files"`
	TotalPages int `json:"total_pages"`
}

// Options controls sorting and pagination.
type Options struct {
	Sort     string // name | date | size ("" = date)
	Order    string // asc | desc ("" = desc for date, asc otherwise)
	Page     int    // 1-based
	PageSize int    // files per page
}

// Result is the JSON response of the browse API.
//
// Pagination applies to files only; directories are always returned in
// full so folders never disappear because of media pagination.
type Result struct {
	Path        string       `json:"path"`
	Breadcrumbs []Breadcrumb `json:"breadcrumbs"`
	Directories []Directory  `json:"directories"`
	Files       []Entry      `json:"files"`
	Pagination  Pagination   `json:"pagination"`
}

// Browse scans the requested directory (fresh, on every call), filters to
// supported media, sorts and paginates.
func (g *Gallery) Browse(vpath string, opts Options) (*Result, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	clean, err := SanitizePath(vpath)
	if err != nil {
		return nil, err
	}
	dirPath, err := g.resolve(clean)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(dirPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		if errors.Is(err, fs.ErrPermission) {
			return nil, ErrNoAccess
		}
		return nil, fmt.Errorf("gallery: stat %q: %w", clean, err)
	}
	if !fi.IsDir() {
		return nil, ErrNotFound
	}

	f, err := os.Open(dirPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, ErrNoAccess
		}
		return nil, fmt.Errorf("gallery: open %q: %w", clean, err)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("gallery: read dir %q: %w", clean, err)
	}

	dirs := make([]Directory, 0, len(entries))
	files := make([]Entry, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // hidden entries are never listed
		}
		if de.IsDir() {
			dirs = append(dirs, Directory{Name: name, Path: joinVirtual(clean, name)})
			continue
		}
		// Only regular files are considered. Symlinks (de.Type() has
		// ModeSymlink), devices, FIFOs etc. are skipped entirely; they
		// can never be listed, and OpenMedia re-checks containment.
		if !de.Type().IsRegular() {
			continue
		}
		mi, ok := classify(g.media, name)
		if !ok {
			continue // unsupported extension: ignore
		}
		info, err := de.Info()
		if err != nil {
			continue // file vanished between ReadDir and Info: skip
		}
		files = append(files, Entry{
			Name:     name,
			Path:     joinVirtual(clean, name),
			Type:     mi.kind,
			MimeType: mi.mimeType,
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
	}

	sortDirs(dirs, opts.Order)
	sortFiles(files, opts.Sort, opts.Order)

	total := len(files)
	pages := 0
	if total > 0 {
		pages = (total + opts.PageSize - 1) / opts.PageSize
	}
	pageFiles := files
	if start := (opts.Page - 1) * opts.PageSize; start >= total {
		pageFiles = []Entry{}
	} else {
		end := start + opts.PageSize
		if end > total {
			end = total
		}
		pageFiles = files[start:end]
	}

	return &Result{
		Path:        clean,
		Breadcrumbs: breadcrumbs(clean),
		Directories: dirs,
		Files:       pageFiles,
		Pagination: Pagination{
			Page:       opts.Page,
			PageSize:   opts.PageSize,
			TotalFiles: total,
			TotalPages: pages,
		},
	}, nil
}

// OpenMedia opens a supported media file for streaming. The returned file
// must be closed by the caller. The path is validated and resolved exactly
// like Browse; the result is guaranteed to be a regular file inside the
// media root with a supported extension.
func (g *Gallery) OpenMedia(vpath string) (*os.File, os.FileInfo, error) {
	clean, err := SanitizePath(vpath)
	if err != nil {
		return nil, nil, err
	}
	name := clean[strings.LastIndex(clean, "/")+1:]
	if strings.HasPrefix(name, ".") {
		return nil, nil, ErrNotFound // hidden files are not served
	}
	mi, ok := classify(g.media, name)
	if !ok {
		return nil, nil, ErrUnsupported
	}
	resolved, err := g.resolve(clean)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, nil, ErrNoAccess
		}
		return nil, nil, fmt.Errorf("gallery: open media %q: %w", clean, err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("gallery: stat media %q: %w", clean, err)
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, ErrNotFound
	}
	_ = mi // mime type is looked up again by the server for the header
	return f, fi, nil
}

// MimeType returns the MIME type for a filename, or "" if unsupported.
func (g *Gallery) MimeType(name string) string {
	if mi, ok := classify(g.media, name); ok {
		return mi.mimeType
	}
	return ""
}

// validate normalizes and checks browse options.
func (o *Options) validate() error {
	switch o.Sort {
	case "":
		o.Sort = SortDate
	case SortName, SortDate, SortSize:
	default:
		return ErrBadOptions
	}
	switch o.Order {
	case "":
		if o.Sort == SortDate {
			o.Order = OrderDesc
		} else {
			o.Order = OrderAsc
		}
	case OrderAsc, OrderDesc:
	default:
		return ErrBadOptions
	}
	if o.Page < 1 || o.Page > 1_000_000 {
		return ErrBadOptions
	}
	if o.PageSize < 1 || o.PageSize > maxPageSize {
		return ErrBadOptions
	}
	return nil
}

// joinVirtual appends a name to a canonical virtual path.
func joinVirtual(base, name string) string {
	if base == "/" {
		return "/" + name
	}
	return base + "/" + name
}

// breadcrumbs builds the Home -> a -> b trail for a canonical path.
func breadcrumbs(clean string) []Breadcrumb {
	crumbs := []Breadcrumb{{Name: "Home", Path: "/"}}
	if clean == "/" {
		return crumbs
	}
	acc := ""
	for _, seg := range strings.Split(clean[1:], "/") {
		acc = joinVirtual(acc, seg)
		crumbs = append(crumbs, Breadcrumb{Name: seg, Path: acc})
	}
	return crumbs
}

// nameCompare is a deterministic case-insensitive filename comparison.
func nameCompare(a, b Entry) int {
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

func compareEntries(a, b Entry, sortBy string) int {
	switch sortBy {
	case SortName:
		return nameCompare(a, b)
	case SortSize:
		switch {
		case a.Size < b.Size:
			return -1
		case a.Size > b.Size:
			return 1
		}
		return nameCompare(a, b)
	default: // SortDate
		switch {
		case a.Modified.Before(b.Modified):
			return -1
		case a.Modified.After(b.Modified):
			return 1
		}
		return nameCompare(a, b)
	}
}

func sortFiles(files []Entry, sortBy, order string) {
	sort.SliceStable(files, func(i, j int) bool {
		return compareEntries(files[i], files[j], sortBy) < 0
	})
	if order == OrderDesc {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}
}

func sortDirs(dirs []Directory, order string) {
	sort.SliceStable(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	if order == OrderDesc {
		for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
			dirs[i], dirs[j] = dirs[j], dirs[i]
		}
	}
}
