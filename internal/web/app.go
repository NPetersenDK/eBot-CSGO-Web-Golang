// Package web holds the HTTP handlers and page rendering.
package web

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/live"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const flashCookie = "ebot_flash"

// App is the dependency bundle every handler hangs off.
type App struct {
	Cfg   *config.Config
	Store *store.Store
	Auth  *auth.Manager
	Cmd   *ebotcmd.Publisher
	Live  *live.Hub
	Log   *slog.Logger

	pages  map[string]*template.Template
	assets fs.FS
}

// Flash is a one-shot message shown on the next page.
type Flash struct {
	// Kind is "ok", "warn" or "err".
	Kind    string
	Message string
}

// View is what every template receives as ".". Page data is in .Data.
type View struct {
	Title   string
	Brand   string
	Path    string
	User    *auth.Session
	IsAdmin bool
	Flash   *Flash
	Cfg     *config.Config
	Data    any
}

// extraFuncs lets a handler file add template functions from its own init().
var extraFuncs = template.FuncMap{}

func registerFunc(name string, fn any) { extraFuncs[name] = fn }

// NewApp parses the templates under assets ("templates/*.html"). layout.html
// wraps every other file. A page file defines {{define "content"}}.
func NewApp(a *App, assets fs.FS) error {
	a.assets = assets
	if a.Log == nil {
		a.Log = slog.Default()
	}
	funcs := template.FuncMap{
		"statusLabel": store.StatusLabel,
		"statusClass": store.StatusClass,
		"isRunning":   store.IsRunning,
		"add":         func(a, b int) int { return a + b },
		"sub":         func(a, b int) int { return a - b },
		"date":        fmtTime("2006-01-02"),
		"datetime":    fmtTime("2006-01-02 15:04"),
		"join":        strings.Join,
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
	}
	for k, v := range extraFuncs {
		funcs[k] = v
	}

	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return err
	}
	a.pages = map[string]*template.Template{}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if name == "layout" {
			continue
		}
		t, err := template.New("layout").Funcs(funcs).ParseFS(assets, "templates/layout.html", f)
		if err != nil {
			return fmt.Errorf("parse %s: %w", f, err)
		}
		a.pages[name] = t
	}
	return nil
}

func fmtTime(layout string) func(any) string {
	return func(v any) string {
		switch t := v.(type) {
		case time.Time:
			if t.IsZero() {
				return ""
			}
			return t.Format(layout)
		case *time.Time:
			if t == nil || t.IsZero() {
				return ""
			}
			return t.Format(layout)
		}
		return ""
	}
}

// Render writes page with status 200.
func (a *App) Render(w http.ResponseWriter, r *http.Request, page string, data any) {
	a.RenderStatus(w, r, http.StatusOK, page, data)
}

// RenderStatus renders page (templates/<page>.html) inside the layout. A data
// value that has a Title() string method sets the page title.
func (a *App) RenderStatus(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	t, ok := a.pages[page]
	if !ok {
		a.Log.Error("unknown page template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	v := View{
		Brand: a.Cfg.Title,
		Path:  r.URL.Path,
		Cfg:   a.Cfg,
		Data:  data,
		Flash: a.takeFlash(w, r),
	}
	if s, ok := auth.FromContext(r.Context()); ok {
		v.User = &s
		v.IsAdmin = s.IsAdmin()
	}
	if ti, ok := data.(interface{ Title() string }); ok {
		v.Title = ti.Title()
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		a.Log.Error("render", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// SetFlash queues a message for the next page view.
func (a *App) SetFlash(w http.ResponseWriter, kind, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(kind + ":" + msg)),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		Secure:   !a.Cfg.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) takeFlash(w http.ResponseWriter, r *http.Request) *Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	kind, msg, ok := strings.Cut(string(b), ":")
	if !ok || (kind != "ok" && kind != "warn" && kind != "err") {
		return nil
	}
	return &Flash{Kind: kind, Message: msg}
}

// Redirect sends a 303 to a local path.
func Redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// NotFound renders the 404 page.
func (a *App) NotFound(w http.ResponseWriter, r *http.Request) {
	a.RenderStatus(w, r, http.StatusNotFound, "error", ErrorPage{Code: 404, Heading: "Not found", Text: "That page does not exist."})
}

// ServerError logs err and renders the 500 page.
func (a *App) ServerError(w http.ResponseWriter, r *http.Request, err error) {
	a.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	a.RenderStatus(w, r, http.StatusInternalServerError, "error", ErrorPage{Code: 500, Heading: "Something went wrong", Text: "The error was logged."})
}

// ErrorPage is the data for templates/error.html.
type ErrorPage struct {
	Code    int
	Heading string
	Text    string
}

func (e ErrorPage) Title() string { return e.Heading }
