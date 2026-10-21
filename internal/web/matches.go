package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const (
	matchesPerPage = 12
	homeMatches    = 10
	homePlayers    = 10
	maxListPage    = 1 << 20
)

// matchStore is the part of store.Store the match pages use.
type matchStore interface {
	ListMatches(ctx context.Context, f store.MatchFilter) ([]store.Match, store.Page, error)
	GetMatch(ctx context.Context, id int64) (*store.Match, error)
	CreateMatch(ctx context.Context, in store.MatchInput) (int64, error)
	UpdateMatch(ctx context.Context, id int64, in store.MatchInput) error
	DeleteMatch(ctx context.Context, id int64) error
	StartMatch(ctx context.Context, id, serverID int64) error
	AssignServer(ctx context.Context, id, serverID int64) error
	RetryMatch(ctx context.Context, id int64) error
	ResetMatch(ctx context.Context, id int64) error
	ArchiveMatch(ctx context.Context, id int64) error
	DuplicateMatch(ctx context.Context, id int64) (int64, error)

	ListMaps(ctx context.Context, matchID int64) ([]store.Map, error)
	ListMapScores(ctx context.Context, mapID int64) ([]store.MapScore, error)
	GetMapScore(ctx context.Context, id int64) (*store.MapScore, error)
	UpdateMapScore(ctx context.Context, id int64, in store.MapScoreInput) error
	ListPlayers(ctx context.Context, matchID, mapID int64) ([]store.Player, error)
	ListRoundSummaries(ctx context.Context, matchID, mapID int64) ([]store.RoundSummary, error)

	ListServers(ctx context.Context) ([]store.Server, error)
	FreeServers(ctx context.Context) ([]store.Server, error)
	GetServer(ctx context.Context, id int64) (*store.Server, error)
	ListTeams(ctx context.Context) ([]store.Team, error)
	ListSeasons(ctx context.Context) ([]store.Season, error)
	GlobalStats(ctx context.Context) (store.GlobalStats, error)
	ListPlayerStats(ctx context.Context, seasonID int64, limit int) ([]store.PlayerStat, error)
	ListPlayerMatches(ctx context.Context, steamID string) ([]store.PlayerMatch, error)
}

// commandSender delivers a live command to the bot.
type commandSender interface {
	Send(ctx context.Context, matchID int64, action, arg, serverIP, authkey string) error
}

type matchHandlers struct {
	app   *App
	st    matchStore
	cmd   commandSender
	cache *statsCache
}

func (a *App) registerMatchRoutes(mux *http.ServeMux) {
	h := &matchHandlers{app: a, st: a.Store, cache: newStatsCache()}
	if a.Cmd != nil {
		h.cmd = a.Cmd
	}
	h.register(mux)
}

func (h *matchHandlers) register(mux *http.ServeMux) {
	user := func(f http.HandlerFunc) http.Handler { return auth.RequireUser(f) }
	admin := func(f http.HandlerFunc) http.Handler { return auth.RequireAdmin(f) }

	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /matches", h.list)
	mux.HandleFunc("GET /matches/{id}", h.view)
	mux.HandleFunc("GET /stats", h.stats)
	mux.HandleFunc("GET /stats/players/{steamid}", h.player)

	mux.Handle("GET /admin/matches/new", admin(h.newForm))
	mux.Handle("POST /admin/matches", admin(h.create))
	mux.Handle("GET /admin/matches/{id}/edit", admin(h.editForm))
	mux.Handle("POST /admin/matches/{id}", admin(h.update))
	mux.Handle("POST /admin/matches/{id}/start", admin(h.start))
	mux.Handle("POST /admin/matches/{id}/assign-server", admin(h.assignServer))
	mux.Handle("POST /admin/matches/{id}/retry", admin(h.retry))
	mux.Handle("POST /admin/matches/{id}/reset", admin(h.reset))
	mux.Handle("POST /admin/matches/{id}/archive", admin(h.archive))
	mux.Handle("POST /admin/matches/{id}/duplicate", admin(h.duplicate))
	mux.Handle("POST /admin/matches/{id}/delete", admin(h.remove))
	mux.Handle("GET /admin/matches/{id}/scores", user(h.scores))
	mux.Handle("POST /admin/matches/{id}/scores/{scoreId}", admin(h.updateScore))
	mux.Handle("POST /admin/matches/{id}/command", admin(h.command))
}

func matchID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// Action is one button in the admin panel of a match.
type Action struct {
	Label string
	// Command is a live bot command, sent through the command endpoint.
	Command string
	// Path is a plain POST route.
	Path string
	// Href is a plain link.
	Href string
	// Style is "", "primary", "danger" or "ghost".
	Style       string
	Confirm     bool
	Disabled    bool
	NeedsServer bool
}

// matchActions translates the button logic of the old panel: which buttons a
// match gets depends on whether the bot owns it and on its status.
func matchActions(m store.Match) []Action {
	base := fmt.Sprintf("/admin/matches/%d/", m.ID)
	post := func(label, route, style string, confirm bool) Action {
		return Action{Label: label, Path: base + route, Style: style, Confirm: confirm}
	}
	link := func(label, href, style string) Action {
		return Action{Label: label, Href: href, Style: style}
	}
	var out []Action
	add := func(a Action) {
		if a.Command != "" && !ebotcmd.ValidAction(a.Command) {
			return
		}
		out = append(out, a)
	}
	command := func(label, c, style string, confirm bool) Action {
		return Action{Label: label, Command: c, Style: style, Confirm: confirm}
	}

	if !m.Enable {
		switch {
		case m.Status == store.StatusEndMatch:
			add(post("Archive", "archive", "primary", false))
			add(post("Delete", "delete", "danger", true))
		case m.Status == store.StatusArchived:
			add(post("Delete", "delete", "danger", true))
		case m.Status != store.StatusNotStarted:
			add(post("Restart", "retry", "primary", false))
			reset := "Reset"
			if m.CurrentMap != nil && m.CurrentMap.Name != "" {
				reset += " " + m.CurrentMap.Name
			}
			add(post(reset, "reset", "", true))
			add(link("Edit", base+"edit", ""))
			add(post("Delete", "delete", "danger", true))
		default:
			start := post("Start", "start", "primary", false)
			start.NeedsServer = true
			add(start)
			add(link("Edit", base+"edit", ""))
			add(post("Delete", "delete", "danger", true))
		}
		add(post("Duplicate", "duplicate", "", false))
		return out
	}

	add(command("Stop", ebotcmd.ActionStopNoRestart, "danger", true))
	add(command("Stop with restart", ebotcmd.ActionStop, "danger", true))
	if m.MapSelectionMode != "" && m.MapSelectionMode != "normal" {
		add(command("Skip map", "skipmap", "", true))
	}
	switch m.ConfigStreamer {
	case 1:
		add(command("Streamer ready", "streamerready", "primary", false))
	case 2:
		a := command("Streamer ready", "streamerready", "primary", false)
		a.Disabled = true
		add(a)
	}
	add(link("RCON and backups", fmt.Sprintf("/matches/%d#console", m.ID), "primary"))

	switch m.Status {
	case store.StatusWUKnife:
		add(command("Start knife", "forceknife", "", false))
		add(command("Skip knife", "passknife", "", false))
	case store.StatusKnife:
		add(command("End knife", "forceknifeend", "", false))
	case store.StatusWU1Side, store.StatusWU2Side, store.StatusWUOT1, store.StatusWUOT2:
		add(command("End warmup or halftime", "forcestart", "", false))
	case store.StatusFirstSide, store.StatusSecondSide, store.StatusOTFirst, store.StatusOTSecond:
		add(command("Stop to warmup", "stopback", "", true))
		add(command("Pause or unpause", "pauseunpause", "", false))
	}
	return out
}

// controllable is true when the bot can address the server: it keys matches on
// a numeric "ip:port".
func controllable(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return false
	}
	return net.ParseIP(host) != nil
}

type matchRow struct {
	Match   store.Match
	Actions []Action
}

func (h *matchHandlers) rows(ms []store.Match, admin bool) []matchRow {
	rows := make([]matchRow, len(ms))
	for i, m := range ms {
		rows[i].Match = m
		if admin {
			rows[i].Actions = matchActions(m)
		}
	}
	return rows
}

func isAdminRequest(r *http.Request) bool {
	s, ok := auth.FromContext(r.Context())
	return ok && s.IsAdmin()
}

type homePage struct {
	Rows []matchRow
	// Players is the top of the player table. It is empty when the numbers
	// could not be read, and the page still renders.
	Players []store.PlayerStat
}

func (homePage) Title() string { return "" }

func (h *matchHandlers) home(w http.ResponseWriter, r *http.Request) {
	ms, _, err := h.st.ListMatches(r.Context(), store.MatchFilter{Page: 1, PerPage: homeMatches})
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	page := homePage{Rows: h.rows(ms, false)}
	if ps, err := h.cache.Players(r.Context(), h.st, 0, homePlayers); err != nil {
		h.app.Log.Error("player stats", "err", err)
	} else {
		page.Players = ps
	}
	h.app.Render(w, r, "home", page)
}

type statsPage struct {
	Stats   store.GlobalStats
	Players []store.PlayerStat
	Seasons []store.Season
	Season  int64
}

func (statsPage) Title() string { return "Statistics" }

func (h *matchHandlers) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seasons, err := h.st.ListSeasons(ctx)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	// Only a season that exists reaches the aggregate query, so an arbitrary
	// ?season= cannot fill the cache or run an expensive query for nothing.
	season := positiveInt(r.URL.Query().Get("season"))
	if !slices.ContainsFunc(seasons, func(s store.Season) bool { return s.ID == season }) {
		season = 0
	}
	g, err := h.cache.Global(ctx, h.st)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	players, err := h.cache.Players(ctx, h.st, season, 0)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	h.app.Render(w, r, "stats", statsPage{Stats: g, Players: players, Seasons: seasons, Season: season})
}

type matchListPage struct {
	Archived bool
	Rows     []matchRow
	Page     store.Page
	Seasons  []store.Season
	Teams    []store.Team
	Season   int64
	Team     int64
	Query    string
	PrevURL  string
	NextURL  string
}

func (p matchListPage) Title() string {
	if p.Archived {
		return "Archived matches"
	}
	return "Matches"
}

func positiveInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (h *matchHandlers) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.MatchFilter{
		Archived: q.Get("status") == "archived",
		SeasonID: positiveInt(q.Get("season")),
		TeamID:   positiveInt(q.Get("team")),
		Search:   strings.TrimSpace(q.Get("q")),
		Page:     int(min(positiveInt(q.Get("page")), maxListPage)),
		PerPage:  matchesPerPage,
	}
	if r := []rune(f.Search); len(r) > 100 {
		f.Search = string(r[:100])
	}
	f.Page = max(1, min(f.Page, maxListPage))
	ms, page, err := h.st.ListMatches(ctx, f)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	seasons, err := h.st.ListSeasons(ctx)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	teams, err := h.st.ListTeams(ctx)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	data := matchListPage{
		Archived: f.Archived,
		Rows:     h.rows(ms, isAdminRequest(r)),
		Page:     page,
		Seasons:  seasons,
		Teams:    teams,
		Season:   f.SeasonID,
		Team:     f.TeamID,
		Query:    f.Search,
	}
	link := func(p int) string {
		v := url.Values{}
		if f.Archived {
			v.Set("status", "archived")
		}
		if f.SeasonID > 0 {
			v.Set("season", strconv.FormatInt(f.SeasonID, 10))
		}
		if f.TeamID > 0 {
			v.Set("team", strconv.FormatInt(f.TeamID, 10))
		}
		if f.Search != "" {
			v.Set("q", f.Search)
		}
		v.Set("page", strconv.Itoa(p))
		return "/matches?" + v.Encode()
	}
	if page.HasPrev() {
		data.PrevURL = link(page.Prev())
	}
	if page.HasNext() {
		data.NextURL = link(page.Next())
	}
	h.app.Render(w, r, "matches", data)
}

type playerRow struct {
	store.Player
	KD      string
	HSPct   string
	Clutchs int
}

type mapView struct {
	Map     store.Map
	Current bool
	Scores  []store.MapScore
	TeamA   []playerRow
	TeamB   []playerRow
	Others  []playerRow
	Rounds  []store.RoundSummary
	DemoURL string
}

type matchViewPage struct {
	Match        store.Match
	Maps         []mapView
	Actions      []Action
	FreeServers  []store.Server
	ServerAddr   string
	Controllable bool
	ShowHostWarn bool
	PlayedRounds int
	LogExists    bool
	ShowEditLink bool
}

func (p matchViewPage) Title() string {
	return fmt.Sprintf("Match #%d", p.Match.ID)
}

func ratio(a, b int) string {
	if b == 0 {
		return strconv.Itoa(a)
	}
	return fmt.Sprintf("%.2f", float64(a)/float64(b))
}

func percent(a, b int) string {
	if b == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(a)*100/float64(b))
}

func newPlayerRow(p store.Player) playerRow {
	return playerRow{
		Player:  p,
		KD:      ratio(p.Kills, p.Deaths),
		HSPct:   percent(p.Headshots, p.Kills),
		Clutchs: p.Clutch1v1 + p.Clutch1v2 + p.Clutch1v3 + p.Clutch1v4 + p.Clutch1v5,
	}
}

func (h *matchHandlers) view(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	ctx := r.Context()
	m, err := h.st.GetMatch(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	maps, err := h.st.ListMaps(ctx, id)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	players, err := h.st.ListPlayers(ctx, id, 0)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	rounds, err := h.st.ListRoundSummaries(ctx, id, 0)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}

	data := matchViewPage{Match: *m, PlayedRounds: m.ScoreA + m.ScoreB}
	for _, mp := range maps {
		mv := mapView{Map: mp, Current: m.CurrentMapID != nil && *m.CurrentMapID == mp.ID}
		mv.Scores, err = h.st.ListMapScores(ctx, mp.ID)
		if err != nil {
			h.app.ServerError(w, r, err)
			return
		}
		if h.app.Cfg.DemoDownload && mp.TVRecordFile != "" {
			mv.DemoURL = fmt.Sprintf("/matches/%d/demo/%d", id, mp.ID)
		}
		for _, p := range players {
			if p.MapID != mp.ID {
				continue
			}
			row := newPlayerRow(p)
			switch p.Team {
			case "a":
				mv.TeamA = append(mv.TeamA, row)
			case "b":
				mv.TeamB = append(mv.TeamB, row)
			default:
				mv.Others = append(mv.Others, row)
			}
		}
		for _, rs := range rounds {
			if rs.MapID == mp.ID {
				mv.Rounds = append(mv.Rounds, rs)
			}
		}
		data.Maps = append(data.Maps, mv)
	}

	if h.app.Cfg.Mode == "lan" {
		data.ServerAddr = m.IP
	} else {
		data.ServerAddr = m.TVIP
	}

	if isAdminRequest(r) {
		data.Actions = matchActions(*m)
		data.LogExists = logExists(h.app.Cfg, m.ID)
		data.ShowEditLink = !m.Enable
		for _, a := range data.Actions {
			if a.Href == fmt.Sprintf("/admin/matches/%d/edit", m.ID) {
				data.ShowEditLink = false
			}
		}
		data.Controllable = controllable(m.IP)
		data.ShowHostWarn = m.Enable && m.IP != "" && !data.Controllable
		if !m.Enable && m.Status == store.StatusNotStarted {
			data.FreeServers, err = h.st.FreeServers(ctx)
			if err != nil {
				h.app.ServerError(w, r, err)
				return
			}
		}
	}
	h.app.Render(w, r, "match", data)
}
