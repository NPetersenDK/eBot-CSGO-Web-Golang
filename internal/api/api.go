// Package api is the JSON API ported from eBot-CSGO-API, served under /api/.
package api

import (
	"net/http"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

// Register adds the /api/ routes. It registers nothing when cfg.APIKeys is empty.
func Register(mux *http.ServeMux, cfg *config.Config, st *store.Store, cmd *ebotcmd.Publisher) {}
