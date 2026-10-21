// Package live fans the bot's Redis pub/sub messages out to browsers as
// server-sent events.
package live

import (
	"context"
	"net/http"

	"github.com/redis/go-redis/v9"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
)

type Hub struct{}

func New(cfg *config.Config, rdb *redis.Client, cmd *ebotcmd.Publisher) *Hub { return &Hub{} }

// Run subscribes to the to-websocket channel and blocks until ctx ends.
func (h *Hub) Run(ctx context.Context) error { <-ctx.Done(); return nil }

// Matches streams scope "match" messages. Public.
func (h *Hub) Matches(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", 501) }

// Rcon streams scope "rcon" messages for the match in r.PathValue("id"). Admin.
func (h *Hub) Rcon(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", 501) }

// Logger streams scope "logger" messages for r.PathValue("id"). Admin.
func (h *Hub) Logger(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", 501) }

// Alive streams scope "alive" messages. Admin.
func (h *Hub) Alive(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", 501) }
