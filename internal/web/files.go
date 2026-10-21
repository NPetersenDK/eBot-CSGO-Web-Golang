package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type mapStore interface {
	GetMap(ctx context.Context, id int64) (*store.Map, error)
}

type logPage struct{ ID int64 }

func (p logPage) Title() string { return fmt.Sprintf("Match log #%d", p.ID) }

func (a *App) registerFileRoutes(mux *http.ServeMux) { a.mountFiles(mux, a.Store) }

func (a *App) mountFiles(mux *http.ServeMux, maps mapStore) {
	mux.HandleFunc("GET /matches/{id}/demo/{mapId}", a.demoHandler(maps))
	mux.Handle("GET /admin/matches/{id}/log", auth.RequireUser(http.HandlerFunc(a.logPageHandler)))
	mux.Handle("GET /admin/matches/{id}/log/raw", auth.RequireUser(http.HandlerFunc(a.logRawHandler)))
}

func positiveID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// safeFileName accepts only a bare file name.
func safeFileName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		name == filepath.Clean(name) && filepath.VolumeName(name) == ""
}

// openInside opens name below dir. The root handle refuses any path that
// leaves dir, symlinks included.
func openInside(dir, name string) (*os.File, os.FileInfo, error) {
	if !safeFileName(name) {
		return nil, nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, os.ErrNotExist
	}
	return f, info, nil
}

func (a *App) demoHandler(maps mapStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Cfg.DemoDownload {
			a.NotFound(w, r)
			return
		}
		matchID, ok1 := positiveID(r.PathValue("id"))
		mapID, ok2 := positiveID(r.PathValue("mapId"))
		if !ok1 || !ok2 {
			a.NotFound(w, r)
			return
		}
		m, err := maps.GetMap(r.Context(), mapID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && m.MatchID != matchID) {
			a.NotFound(w, r)
			return
		}
		if err != nil {
			a.ServerError(w, r, err)
			return
		}
		if !safeFileName(m.TVRecordFile) {
			a.NotFound(w, r)
			return
		}
		name := m.TVRecordFile + ".dem.zip"
		f, info, err := openInside(a.Cfg.DemoPath, name)
		if err != nil {
			a.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, info.ModTime(), f)
	}
}

func logFileName(id int64) string { return fmt.Sprintf("match-%d.html", id) }

func (a *App) logPageHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok || !logExists(a.Cfg, id) {
		a.NotFound(w, r)
		return
	}
	a.Render(w, r, "log", logPage{ID: id})
}

func logExists(cfg *config.Config, id int64) bool {
	f, _, err := openInside(cfg.LogMatchAdminPath, logFileName(id))
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (a *App) logRawHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		a.NotFound(w, r)
		return
	}
	f, _, err := openInside(a.Cfg.LogMatchAdminPath, logFileName(id))
	if err != nil {
		a.NotFound(w, r)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}
