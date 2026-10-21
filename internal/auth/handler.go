package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
)

const (
	stateCookie   = "ebot_oidc_state"
	idTokenCookie = "ebot_id_token"

	idTokenPath      = "/logout"
	maxLoginBody     = 8 << 10
	maxUsernameBytes = 128
	maxLogNameRunes  = 64
)

// logName shortens a user-supplied name for log lines.
func logName(s string) string {
	if r := []rune(s); len(r) > maxLogNameRunes {
		return string(r[:maxLogNameRunes])
	}
	return s
}

// Handler serves /login, /logout and the OIDC routes.
type Handler struct {
	cfg       *config.Config
	m         *Manager
	local     LocalUsers
	render    RenderFunc
	oidc      *oidcClient
	providers []LoginProvider
	limiter   *loginLimiter
}

// RenderFunc renders a page template (login, denied) with the shared layout.
type RenderFunc func(w http.ResponseWriter, r *http.Request, status int, page string, data any)

// NewHandler wires the login routes. OIDC is enabled when cfg.OIDC.Issuer is set.
// Rendering of login.html and denied.html goes through render, which is
// web.App.Render without the request-scoped view data.
func NewHandler(ctx context.Context, cfg *config.Config, m *Manager, local LocalUsers, render RenderFunc) (*Handler, error) {
	providers, err := parseLoginProviders(cfg.LoginProviders)
	if err != nil {
		return nil, err
	}
	h := &Handler{
		cfg:       cfg,
		m:         m,
		local:     local,
		render:    render,
		providers: providers,
		limiter:   newLoginLimiter(10, ipLimit(cfg.TrustProxy), 5*time.Minute, nil),
	}
	if cfg.OIDC.Issuer != "" {
		if h.oidc, err = newOIDCClient(ctx, cfg.OIDC); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// Register adds:
//
//	GET  /login
//	POST /login
//	POST /logout
//	GET  /auth/oidc/login
//	GET  /auth/oidc/callback
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.loginSubmit)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /auth/oidc/login", h.oidcLogin)
	mux.HandleFunc("GET /auth/oidc/callback", h.oidcCallback)
}

// localNext returns p when it is a local path, else "/".
func localNext(p string) string {
	if p == "" || p[0] != '/' {
		return "/"
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return "/"
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f || p[i] == '\\' {
			return "/"
		}
	}
	return p
}

func (h *Handler) page(r *http.Request, next, errMsg string) LoginPage {
	p := LoginPage{Brand: h.cfg.Title, Next: next, Error: errMsg, LocalLogin: !h.cfg.LocalLoginDisabled}
	if h.oidc != nil {
		providers := h.providers
		if len(providers) == 0 {
			providers = []LoginProvider{{Name: "Single sign-on"}}
		}
		for _, pr := range providers {
			q := url.Values{"next": {next}}
			if pr.Hint != "" {
				q.Set("idp", pr.Hint)
			}
			p.Providers = append(p.Providers, LoginProviderLink{Name: pr.Name, URL: "/auth/oidc/login?" + q.Encode()})
		}
	}
	return p
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	next := localNext(r.URL.Query().Get("next"))
	if _, ok := h.m.Read(r); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login", h.page(r, next, ""))
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if h.cfg.LocalLoginDisabled {
		http.Error(w, "local login is disabled", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	if err := r.ParseForm(); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	next := localNext(r.PostForm.Get("next"))
	ip := h.clientIP(r)
	if len(username) > maxUsernameBytes {
		slog.Info("auth: login failed", "user", logName(username), "ip", ip)
		h.render(w, r, http.StatusUnauthorized, "login", h.page(r, next, "Invalid username or password."))
		return
	}
	user := strings.ToLower(username)

	if !h.limiter.allow(user, ip) {
		slog.Warn("auth: login rate limited", "user", logName(username), "ip", ip)
		h.render(w, r, http.StatusTooManyRequests, "login", h.page(r, next, "Too many attempts. Try again later."))
		return
	}
	s, err := h.local.VerifyLocal(r.Context(), username, password)
	if errors.Is(err, ErrInvalidCredentials) {
		slog.Info("auth: login failed", "user", logName(username), "ip", ip)
		h.render(w, r, http.StatusUnauthorized, "login", h.page(r, next, "Invalid username or password."))
		return
	}
	if err != nil {
		h.limiter.refund(user, ip)
		slog.Error("auth: local login error", "err", err)
		h.render(w, r, http.StatusInternalServerError, "login", h.page(r, next, "Login is temporarily unavailable."))
		return
	}
	s.Source = SourceLocal
	if err := h.m.Issue(w, s); err != nil {
		h.limiter.refund(user, ip)
		slog.Error("auth: issue session", "err", err)
		h.render(w, r, http.StatusInternalServerError, "login", h.page(r, next, "Login is temporarily unavailable."))
		return
	}
	h.limiter.success(user, ip)
	slog.Info("auth: login", "user", logName(s.Subject), "role", s.Role, "source", SourceLocal, "ip", ip)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	s, _ := h.m.Read(r)
	h.m.Clear(w)
	h.writeIDToken(w, "")
	h.clearLegacyIDToken(w)
	if h.oidc != nil && s.Source == SourceOIDC {
		if c, err := r.Cookie(idTokenCookie); err == nil && c.Value != "" {
			if u := h.oidc.logoutURL(c.Value, h.postLogoutURL()); u != "" {
				http.Redirect(w, r, u, http.StatusSeeOther)
				return
			}
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// postLogoutURL is the site root, derived from the registered redirect URL.
func (h *Handler) postLogoutURL() string {
	u, err := url.Parse(h.cfg.OIDC.RedirectURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}

// clearLegacyIDToken removes the cookie older versions set on "/".
func (h *Handler) clearLegacyIDToken(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: idTokenCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.m.secure, SameSite: http.SameSiteLaxMode})
}

func (h *Handler) writeIDToken(w http.ResponseWriter, raw string) {
	c := &http.Cookie{
		Name:     idTokenCookie,
		Value:    raw,
		Path:     idTokenPath,
		HttpOnly: true,
		Secure:   h.m.secure,
		SameSite: http.SameSiteLaxMode,
	}
	if raw == "" {
		c.MaxAge = -1
	} else {
		c.MaxAge = int(sessionTTL.Seconds())
	}
	http.SetCookie(w, c)
}

type oidcFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
}

func (h *Handler) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	if err1 != nil || err2 != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	flow, _ := json.Marshal(oidcFlow{State: state, Nonce: nonce, Verifier: verifier, Next: localNext(r.URL.Query().Get("next"))})
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    base64.RawURLEncoding.EncodeToString(flow),
		Path:     "/auth",
		HttpOnly: true,
		Secure:   h.m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	hint := hintFor(h.providers, r.URL.Query().Get("idp"))
	http.Redirect(w, r, h.oidc.authCodeURL(state, hint, nonce, verifier), http.StatusFound)
}

func (h *Handler) readFlow(r *http.Request) (oidcFlow, bool) {
	c, err := r.Cookie(stateCookie)
	if err != nil || c.Value == "" {
		return oidcFlow{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return oidcFlow{}, false
	}
	var f oidcFlow
	if err := json.Unmarshal(raw, &f); err != nil || f.State == "" || f.Nonce == "" || f.Verifier == "" {
		return oidcFlow{}, false
	}
	if f.State != r.URL.Query().Get("state") {
		return oidcFlow{}, false
	}
	return f, true
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		http.NotFound(w, r)
		return
	}
	flow, ok := h.readFlow(r)
	if !ok {
		slog.Warn("auth: oidc state invalid", "ip", h.clientIP(r))
		http.Error(w, "invalid login state, try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth", MaxAge: -1})

	res, err := h.oidc.exchange(r.Context(), r.URL.Query().Get("code"), flow.Nonce, flow.Verifier)
	var denied *deniedError
	if errors.As(err, &denied) {
		slog.Info("auth: oidc login denied", "subject", denied.page.Subject)
		w.Header().Set("Cache-Control", "no-store")
		h.render(w, r, http.StatusForbidden, "denied", denied.page)
		return
	}
	if err != nil {
		slog.Warn("auth: oidc exchange failed", "err", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	if err := h.m.Issue(w, res.Session); err != nil {
		slog.Error("auth: issue session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.writeIDToken(w, res.RawIDToken)
	slog.Info("auth: login", "user", res.Session.Subject, "role", res.Session.Role, "source", SourceOIDC)
	http.Redirect(w, r, localNext(flow.Next), http.StatusSeeOther)
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (h *Handler) clientIP(r *http.Request) string { return clientIP(r, h.cfg.TrustProxy) }

// clientIP is the right-most X-Forwarded-For hop when the proxy is trusted
// (EBOT_TRUST_PROXY), else the peer address. The header may come as several
// lines, so the last value of the last line counts.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		values := r.Header.Values("X-Forwarded-For")
		for i := len(values) - 1; i >= 0; i-- {
			parts := strings.Split(values[i], ",")
			for j := len(parts) - 1; j >= 0; j-- {
				if ip := strings.TrimSpace(parts[j]); ip != "" {
					return ip
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipLimit is the failed logins allowed per address in the window. It only makes
// sense when the address is the real visitor, which needs EBOT_TRUST_PROXY
// behind a proxy.
func ipLimit(trustProxy bool) int {
	if trustProxy {
		return 30
	}
	return 0
}
