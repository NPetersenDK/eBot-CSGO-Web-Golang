// Package api is the JSON API ported from eBot-CSGO-API, served under /api/.
package api

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

//go:embed openapi.json
var openapiSpec []byte

const (
	maxBodyBytes       = 1 << 20
	defaultStopTimeout = 15 * time.Second
	stopPollInterval   = 250 * time.Millisecond
)

// matchStore is the part of the store the API uses.
type matchStore interface {
	Ping(ctx context.Context) error

	ListMatches(ctx context.Context, f store.MatchFilter) ([]store.Match, store.Page, error)
	GetMatch(ctx context.Context, id int64) (*store.Match, error)
	CreateMatch(ctx context.Context, in store.MatchInput) (int64, error)
	DeleteMatch(ctx context.Context, id int64) error
	StartMatch(ctx context.Context, id, serverID int64) error
	AssignServer(ctx context.Context, id, serverID int64) error
	ResetMatch(ctx context.Context, id int64) error
	ArchiveMatch(ctx context.Context, id int64) error

	ListMaps(ctx context.Context, matchID int64) ([]store.Map, error)
	ListPlayers(ctx context.Context, matchID, mapID int64) ([]store.Player, error)
	ListRoundSummaries(ctx context.Context, matchID, mapID int64) ([]store.RoundSummary, error)

	ListServers(ctx context.Context) ([]store.Server, error)
	FreeServers(ctx context.Context) ([]store.Server, error)
	GetServer(ctx context.Context, id int64) (*store.Server, error)
	CreateServers(ctx context.Context, in store.ServerCreate) (int, error)
	UpdateServer(ctx context.Context, id int64, in store.ServerInput) error
	DeleteServer(ctx context.Context, id int64) error

	ListTeams(ctx context.Context) ([]store.Team, error)
	GetTeam(ctx context.Context, id int64) (*store.Team, error)
	ListSeasons(ctx context.Context) ([]store.Season, error)
}

// commander sends live commands to the bot.
type commander interface {
	Send(ctx context.Context, matchID int64, action, arg, serverIP, authkey string) error
}

type deps struct {
	store       matchStore
	cmd         commander
	keys        []string
	stopTimeout time.Duration
	stopPoll    time.Duration
}

type server struct {
	deps
}

// Register adds the /api/ routes. It registers nothing when cfg.APIKeys is empty.
func Register(mux *http.ServeMux, cfg *config.Config, st *store.Store, cmd *ebotcmd.Publisher) {
	d := deps{store: st, keys: cfg.APIKeys}
	if cmd != nil {
		d.cmd = cmd
	}
	newServer(d).routes(mux)
}

func newServer(d deps) *server {
	if d.stopTimeout == 0 {
		d.stopTimeout = defaultStopTimeout
	}
	if d.stopPoll == 0 {
		d.stopPoll = stopPollInterval
	}
	return &server{deps: d}
}

func (s *server) routes(mux *http.ServeMux) {
	if len(s.keys) == 0 {
		return
	}
	pub := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, h) }
	priv := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }

	pub("GET /api/health", s.health)
	pub("GET /api/openapi.json", serveSpec)

	priv("GET /api/matches", s.listMatches)
	priv("POST /api/matches", s.createMatch)
	priv("GET /api/matches/{id}", s.getMatch)
	priv("DELETE /api/matches/{id}", s.deleteMatch)
	priv("POST /api/matches/{id}/start", s.startMatch)
	priv("POST /api/matches/{id}/archive", s.archiveMatch)
	priv("POST /api/matches/{id}/stop", s.stopMatch)
	priv("POST /api/matches/{id}/reset", s.resetMatch)
	priv("POST /api/matches/{id}/restart", s.restartMatch)
	priv("GET /api/matches/{id}/maps", s.listMatchMaps)
	priv("GET /api/matches/{id}/players", s.listMatchPlayers)
	priv("GET /api/matches/{id}/rounds", s.listMatchRounds)
	priv("GET /api/matches/{id}/kills", s.listMatchKills)

	priv("GET /api/servers", s.listServers)
	priv("POST /api/servers", s.createServer)
	priv("GET /api/servers/{id}", s.getServer)
	priv("PUT /api/servers/{id}", s.updateServer)
	priv("DELETE /api/servers/{id}", s.deleteServer)

	priv("GET /api/teams", s.listTeams)
	priv("GET /api/seasons", s.listSeasons)
}

func serveSpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(openapiSpec)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// auth checks the X-API-Key header against every configured key without
// stopping at the first match.
func (s *server) auth(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("X-API-Key"))
		ok := 0
		for _, k := range s.keys {
			ok |= subtle.ConstantTimeCompare(got, []byte(k))
		}
		if ok != 1 {
			writeError(w, http.StatusUnauthorized, "invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func notImplemented(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotImplemented, what+" is not supported yet")
}

// storeError maps store errors onto status codes. notFound is the 404 text.
func storeError(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, notFound)
	case errors.Is(err, errors.ErrUnsupported):
		writeError(w, http.StatusNotImplemented, "not implemented")
	case errors.Is(err, store.ErrMatchEnabled):
		writeError(w, http.StatusConflict, "match is running")
	case errors.Is(err, store.ErrServerBusy):
		writeError(w, http.StatusConflict, "another match is running on that server")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("api store error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// decodeJSON reads a size-limited JSON body with a JSON content type.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
