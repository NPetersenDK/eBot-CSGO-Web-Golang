package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const sessionTTL = 12 * time.Hour

// Manager signs, writes and reads the session cookie.
type Manager struct {
	key    []byte
	secure bool
}

func NewManager(key []byte, secure bool) *Manager { return &Manager{key: key, secure: secure} }

type sessionClaims struct {
	Sub  string `json:"sub"`
	Name string `json:"name"`
	Role string `json:"role"`
	Src  string `json:"src"`
	Exp  int64  `json:"exp"`
}

// Issue writes the session cookie (12 hours, HttpOnly, SameSite=Lax).
func (m *Manager) Issue(w http.ResponseWriter, s Session) error {
	exp := time.Now().Add(sessionTTL)
	payload, err := json.Marshal(sessionClaims{Sub: s.Subject, Name: s.Name, Role: s.Role, Src: s.Source, Exp: exp.Unix()})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    m.sign(payload),
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
	return nil
}

// Read returns the session in the request cookie, if it is valid and not expired.
func (m *Manager) Read(r *http.Request) (Session, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, false
	}
	claims, ok := m.verify(c.Value)
	if !ok || claims.Sub == "" || time.Now().Unix() > claims.Exp {
		return Session{}, false
	}
	if claims.Role != RoleAdmin && claims.Role != RoleViewer {
		return Session{}, false
	}
	if claims.Src != SourceLocal && claims.Src != SourceOIDC {
		return Session{}, false
	}
	return Session{Subject: claims.Sub, Name: claims.Name, Role: claims.Role, Source: claims.Src, Expires: time.Unix(claims.Exp, 0)}, true
}

func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Middleware attaches the session to the request context. For local sessions
// it asks roles whether the user is still an admin and drops the session
// otherwise. roles may be nil.
func (m *Manager) Middleware(roles RoleChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, ok := m.Read(r)
			if ok && s.Source == SourceLocal && roles != nil {
				admin, err := roles.IsAdmin(r.Context(), s.Subject)
				switch {
				case err == nil && admin:
				case err == nil || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrInvalidCredentials):
					slog.Info("auth: local session dropped", "user", s.Subject)
					m.Clear(w)
					ok = false
				default:
					slog.Error("auth: role check failed, keeping session", "user", s.Subject, "err", err)
				}
			}
			if ok {
				r = r.WithContext(WithSession(r.Context(), s))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Manager) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, m.key)
	h.Write(payload)
	return h.Sum(nil)
}

func (m *Manager) sign(payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(m.mac(payload))
}

func (m *Manager) verify(token string) (sessionClaims, bool) {
	pb, sb, ok := strings.Cut(token, ".")
	if !ok || pb == "" || sb == "" {
		return sessionClaims{}, false
	}
	enc := base64.RawURLEncoding.Strict()
	payload, err := enc.DecodeString(pb)
	if err != nil {
		return sessionClaims{}, false
	}
	sig, err := enc.DecodeString(sb)
	if err != nil {
		return sessionClaims{}, false
	}
	if subtle.ConstantTimeCompare(sig, m.mac(payload)) != 1 {
		return sessionClaims{}, false
	}
	var c sessionClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return sessionClaims{}, false
	}
	return c, true
}
