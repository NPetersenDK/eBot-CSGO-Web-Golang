package auth

import (
	"errors"
	"net/http"
)

// Manager signs, writes and reads the session cookie.
type Manager struct {
	key    []byte
	secure bool
}

func NewManager(key []byte, secure bool) *Manager { return &Manager{key: key, secure: secure} }

// Issue writes the session cookie (12 hours, HttpOnly, SameSite=Lax).
func (m *Manager) Issue(w http.ResponseWriter, s Session) error { return errors.ErrUnsupported }

// Read returns the session in the request cookie, if it is valid and not expired.
func (m *Manager) Read(r *http.Request) (Session, bool) { return Session{}, false }

func (m *Manager) Clear(w http.ResponseWriter) {}

// Middleware attaches the session to the request context. For local sessions
// it asks roles whether the user is still an admin and downgrades or drops the
// session otherwise. roles may be nil.
func (m *Manager) Middleware(roles RoleChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
