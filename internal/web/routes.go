package web

import (
	"io/fs"
	"net/http"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
)

// Register adds every page route. The routes table is in CONTRACTS.md.
func (a *App) Register(mux *http.ServeMux) {
	static, err := fs.Sub(a.assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /live/matches", a.Live.Matches)
	mux.Handle("GET /admin/status", auth.RequireUser(http.HandlerFunc(a.Live.StatusHandler)))
	mux.Handle("GET /admin/live/alive", auth.RequireAdmin(http.HandlerFunc(a.Live.Alive)))
	mux.Handle("GET /admin/live/{id}/rcon", auth.RequireAdmin(http.HandlerFunc(a.Live.Rcon)))
	mux.Handle("GET /admin/live/{id}/logger", auth.RequireAdmin(http.HandlerFunc(a.Live.Logger)))

	a.registerMatchRoutes(mux)
	a.registerCrudRoutes(mux)
	a.registerFileRoutes(mux)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { a.NotFound(w, r) })
}
