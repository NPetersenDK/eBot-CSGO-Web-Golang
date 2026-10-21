package auth

import (
	"context"
	"net/http"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
)

// Handler serves /login, /logout and the OIDC routes.
type Handler struct{}

// NewHandler wires the login routes. OIDC is enabled when cfg.OIDC.Issuer is set.
// Rendering of login.html and denied.html goes through render, which is
// web.App.Render without the request-scoped view data.
func NewHandler(ctx context.Context, cfg *config.Config, m *Manager, local LocalUsers, render RenderFunc) (*Handler, error) {
	return &Handler{}, nil
}

// RenderFunc renders a page template (login, denied) with the shared layout.
type RenderFunc func(w http.ResponseWriter, r *http.Request, status int, page string, data any)

// Register adds:
//
//	GET  /login
//	POST /login
//	GET  /logout, POST /logout
//	GET  /auth/oidc/login
//	GET  /auth/oidc/callback
func (h *Handler) Register(mux *http.ServeMux) {}
