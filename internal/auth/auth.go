// Package auth holds the session, the login flows (local sf_guard accounts and
// OIDC) and the request middleware. It must not import internal/store: the
// store satisfies LocalUsers and RoleChecker instead.
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"

	SourceLocal = "local"
	SourceOIDC  = "oidc"

	CookieName = "ebot_session"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

// Session is what the signed cookie carries.
type Session struct {
	Subject string
	Name    string
	Role    string
	Source  string
	Expires time.Time
}

func (s Session) IsAdmin() bool { return s.Role == RoleAdmin }

// LocalUsers verifies a username and password against sf_guard_user. It
// returns ErrInvalidCredentials for an unknown user, a wrong password, an
// inactive user and a user that is not an admin.
type LocalUsers interface {
	VerifyLocal(ctx context.Context, username, password string) (Session, error)
}

// RoleChecker re-reads the admin flag of a local user on every request, so a
// demotion or deletion takes effect at once.
type RoleChecker interface {
	IsAdmin(ctx context.Context, username string) (bool, error)
}

type ctxKey struct{}

// WithSession stores the session on the context.
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the session attached by Manager.Middleware.
func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(Session)
	return s, ok
}

// RequireUser lets any logged-in user through (admin or viewer). Anonymous
// browser requests go to /login, other requests get 401.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			deny(w, r, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin lets admins through. Viewers get 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := FromContext(r.Context())
		if !ok {
			deny(w, r, http.StatusUnauthorized)
			return
		}
		if !s.IsAdmin() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func deny(w http.ResponseWriter, r *http.Request, code int) {
	if r.Method == http.MethodGet && r.Header.Get("Accept") != "text/event-stream" {
		http.Redirect(w, r, "/login?next="+safeNext(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	http.Error(w, http.StatusText(code), code)
}

// safeNext keeps only a local path, so the login redirect cannot leave the site.
func safeNext(p string) string {
	if len(p) == 0 || p[0] != '/' || (len(p) > 1 && (p[1] == '/' || p[1] == '\\')) {
		return "/"
	}
	return urlEscape(p)
}
