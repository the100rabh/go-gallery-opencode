// Package server implements the HTTP layer: a tiny hand-rolled router
// (no framework), the JSON browse API, media streaming with range support,
// embedded static assets, request logging and consistent error responses.
//
// Error responses never contain internal filesystem paths; detailed errors
// go to the log only.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"media-gallery/internal/gallery"
	"media-gallery/internal/logger"
)

// Server serves the gallery UI, the JSON API and the media files.
type Server struct {
	gal *gallery.Gallery
	log *logger.Logger

	// assets are read once at startup from the embedded filesystem.
	assets map[string]asset
}

type asset struct {
	data []byte
	mime string
}

var assetNames = []struct{ name, mime string }{
	{"index.html", "text/html; charset=utf-8"},
	{"style.css", "text/css; charset=utf-8"},
	{"app.js", "text/javascript; charset=utf-8"},
}

// New builds the server. assets must contain the embedded web files.
func New(gal *gallery.Gallery, log *logger.Logger, assets fs.FS) (*Server, error) {
	s := &Server{gal: gal, log: log, assets: make(map[string]asset, len(assetNames))}
	for _, a := range assetNames {
		data, err := fs.ReadFile(assets, a.name)
		if err != nil {
			return nil, err
		}
		s.assets[a.name] = asset{data: data, mime: a.mime}
	}
	return s, nil
}

// ServeHTTP routes a request. The routing is deliberately tiny; note that
// net/http has already decoded the URL path exactly once by the time we
// see it, and gallery.SanitizePath rejects traversal attempts afterwards.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.setSecurityHeaders(w)

	path := r.URL.Path
	switch {
	case path == "/healthz":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))

	case strings.HasPrefix(path, "/api/"):
		s.routeAPI(w, r)

	case path == "/media" || strings.HasPrefix(path, "/media/"):
		s.handleMedia(w, r)

	case path == "/" || path == "/gallery" || strings.HasPrefix(path, "/gallery/"):
		if !s.method(w, r, http.MethodGet) {
			return
		}
		s.serveAsset(w, r, "index.html")

	case path == "/style.css" || path == "/app.js":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		s.serveAsset(w, r, strings.TrimPrefix(path, "/"))

	default:
		http.Error(w, "Not found.", http.StatusNotFound)
	}
}

func (s *Server) routeAPI(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/api") {
	case "/browse":
		if !s.method(w, r, http.MethodGet) {
			return
		}
		s.handleBrowse(w, r)
	default:
		s.writeJSONError(w, http.StatusNotFound, "Unknown API endpoint.")
	}
}

// method enforces GET/HEAD on read-only endpoints.
func (s *Server) method(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed || (allowed == http.MethodGet && r.Method == http.MethodHead) {
		return true
	}
	w.Header().Set("Allow", allowed)
	http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
	return false
}

// handleBrowse implements GET /api/browse.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := gallery.Options{
		Sort:  q.Get("sort"),
		Order: q.Get("order"),
	}
	var err error
	if opts.Page, err = queryInt(q.Get("page"), 1, 1_000_000); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid page parameter.")
		return
	}
	if opts.PageSize, err = queryInt(q.Get("page_size"), s.gal.PageSize(), 500); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid page_size parameter.")
		return
	}
	res, err := s.gal.Browse(q.Get("path"), opts)
	if err != nil {
		s.writeGalleryError(w, err, "directory", true)
		return
	}
	s.writeJSON(w, http.StatusOK, res)
}

// handleMedia serves the original file for GET /media/<virtual path>.
// http.ServeContent provides Content-Type handling, If-Modified-Since and,
// crucially, HTTP range requests so video seeking works without loading
// anything into memory.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if !s.method(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path == "/media" {
		http.Redirect(w, r, "/gallery/", http.StatusMovedPermanently)
		return
	}
	vpath := strings.TrimPrefix(r.URL.Path, "/media")

	f, fi, err := s.gal.OpenMedia(vpath)
	if err != nil {
		s.writeGalleryError(w, err, "file", false)
		return
	}
	defer f.Close()

	if mt := s.gal.MimeType(fi.Name()); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	if cd := mime.FormatMediaType("inline", map[string]string{"filename": fi.Name()}); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// serveAsset serves an embedded frontend file.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	a, ok := s.assets[name]
	if !ok {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", a.mime)
	w.Header().Set("Cache-Control", "no-store")
	if name == "index.html" {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self'; media-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(a.data)
	}
}

// setSecurityHeaders applies conservative headers to every response.
func (s *Server) setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
}

// queryInt parses an optional positive integer query parameter.
func queryInt(raw string, def, max int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, errors.New("invalid integer")
	}
	return n, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// writeGalleryError maps gallery errors to HTTP responses with generic,
// path-free messages; the detailed error goes to the log. API routes get
// JSON bodies, everything else plain text.
func (s *Server) writeGalleryError(w http.ResponseWriter, err error, what string, asJSON bool) {
	switch {
	case errors.Is(err, gallery.ErrInvalidPath):
		s.log.Warn("rejected path", "what", what, "err", err)
		if asJSON {
			s.writeJSONError(w, http.StatusForbidden, "The requested path is invalid.")
		} else {
			http.Error(w, "The requested path is invalid.", http.StatusForbidden)
		}
	case errors.Is(err, gallery.ErrBadOptions):
		s.writeJSONError(w, http.StatusBadRequest, "Invalid query parameters.")
	case errors.Is(err, gallery.ErrNotFound), errors.Is(err, gallery.ErrUnsupported):
		if asJSON {
			s.writeJSONError(w, http.StatusNotFound, "The requested "+what+" was not found.")
		} else {
			http.Error(w, "The requested "+what+" was not found.", http.StatusNotFound)
		}
	case errors.Is(err, gallery.ErrNoAccess):
		s.log.Warn("access denied", "what", what, "err", err)
		if asJSON {
			s.writeJSONError(w, http.StatusForbidden, "Unable to access this "+what+".")
		} else {
			http.Error(w, "Unable to access this "+what+".", http.StatusForbidden)
		}
	default:
		s.log.Error("internal error", "what", what, "err", err)
		if asJSON {
			s.writeJSONError(w, http.StatusInternalServerError, "An internal error occurred.")
		} else {
			http.Error(w, "An internal error occurred.", http.StatusInternalServerError)
		}
	}
}

// LogRequests wraps a handler with one-line request logging.
// Successful media requests are logged at debug level only (they would be
// far too noisy on a photo gallery); everything else is logged at info,
// failures at warn/error.
func LogRequests(log *logger.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := logger.LevelInfo
		if rec.status >= 500 {
			level = logger.LevelError
		} else if rec.status >= 400 {
			level = logger.LevelWarn
		} else if strings.HasPrefix(r.URL.Path, "/media/") {
			level = logger.LevelDebug // avoid logging every successful media request
		}
		log.Write(level, "request",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"status", rec.status,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"remote", r.RemoteAddr)
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush passes through so streaming responses keep working.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
