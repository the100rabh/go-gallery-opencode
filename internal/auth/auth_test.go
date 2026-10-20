package auth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"media-gallery/internal/logger"
)

func testLogger() *logger.Logger {
	return logger.New(&bytes.Buffer{}, "error")
}

func newHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func request(user, pass string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/browse", nil)
	if user != "" || pass != "" {
		r.SetBasicAuth(user, pass)
	}
	return r
}

func TestAuthDisabled(t *testing.T) {
	// When auth is disabled the middleware is simply not installed;
	// this test documents the expectation for the composed handler.
	w := httptest.NewRecorder()
	newHandler().ServeHTTP(w, request("", ""))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestValidCredentials(t *testing.T) {
	mw := NewBasic("admin", "change-me", testLogger()).Middleware(newHandler())
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, request("admin", "change-me"))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestInvalidCredentials(t *testing.T) {
	mw := NewBasic("admin", "change-me", testLogger()).Middleware(newHandler())
	cases := [][2]string{
		{"admin", "wrong"},
		{"wrong", "change-me"},
		{"", ""},
		{"ADMIN", "change-me"}, // case matters
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, request(c[0], c[1]))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("credentials %v: status = %d, want 401", c, w.Code)
		}
		if got := w.Header().Get("WWW-Authenticate"); got == "" {
			t.Errorf("credentials %v: missing WWW-Authenticate", c)
		}
	}
}

func TestMissingCredentials(t *testing.T) {
	mw := NewBasic("admin", "change-me", testLogger()).Middleware(newHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil) // no Authorization header
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestMalformedAuthHeader(t *testing.T) {
	mw := NewBasic("admin", "change-me", testLogger()).Middleware(newHandler())
	for _, header := range []string{"Basic !!!not-base64!!!", "Bearer abc", "Basic " + "not:user"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status = %d, want 401", header, w.Code)
		}
	}
}

func TestHealthzBypassesAuth(t *testing.T) {
	mw := NewBasic("admin", "change-me", testLogger()).Middleware(newHandler())
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", w.Code)
	}
}
