// Package auth implements the simple HTTP Basic authentication used to
// protect the gallery. It is intentionally minimal: one user, one password,
// both configured in YAML. No accounts, roles, tokens or password resets.
//
// Credential comparison is constant time (SHA-256 digests compared with
// crypto/subtle) so password length and content do not leak through timing.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"media-gallery/internal/logger"
)

const realm = `Basic realm="media-gallery", charset="UTF-8"`

// Basic holds the configured credentials as SHA-256 digests.
type Basic struct {
	userHash [sha256.Size]byte
	passHash [sha256.Size]byte
	log      *logger.Logger
}

// NewBasic creates middleware from the configured username and password.
func NewBasic(username, password string, log *logger.Logger) *Basic {
	return &Basic{
		userHash: sha256.Sum256([]byte(username)),
		passHash: sha256.Sum256([]byte(password)),
		log:      log,
	}
}

// Middleware wraps a handler, requiring valid Basic credentials for every
// request except /healthz (a liveness probe reveals nothing).
func (a *Basic) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !a.validate(user, pass) {
			a.log.Warn("authentication failed", "remote", r.RemoteAddr, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", realm)
			http.Error(w, "Authentication required.", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validate compares credentials in constant time.
func (a *Basic) validate(user, pass string) bool {
	return equalDigest(a.userHash, user) && equalDigest(a.passHash, pass)
}

func equalDigest(digest [sha256.Size]byte, s string) bool {
	h := sha256.Sum256([]byte(s))
	return subtle.ConstantTimeCompare(digest[:], h[:]) == 1
}
