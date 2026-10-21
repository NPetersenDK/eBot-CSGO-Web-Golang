package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type fakeMatchStore struct {
	matches []store.Match
	maps    map[int64][]store.Map
	scores  map[int64][]store.MapScore
	servers []store.Server
	teams   []store.Team
	seasons []store.Season

	created   []store.MatchInput
	updated   []store.MatchInput
	updateErr error
	calls     []string
	filter    store.MatchFilter
}

func (f *fakeMatchStore) find(id int64) *store.Match {
	for i := range f.matches {
		if f.matches[i].ID == id {
			m := f.matches[i]
			return &m
		}
	}
	return nil
}

func (f *fakeMatchStore) ListMatches(_ context.Context, flt store.MatchFilter) ([]store.Match, store.Page, error) {
	f.filter = flt
	return f.matches, store.Page{Page: max(flt.Page, 1), PerPage: flt.PerPage, Total: len(f.matches)}, nil
}
func (f *fakeMatchStore) GetMatch(_ context.Context, id int64) (*store.Match, error) {
	if m := f.find(id); m != nil {
		return m, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeMatchStore) CreateMatch(_ context.Context, in store.MatchInput) (int64, error) {
	f.created = append(f.created, in)
	return 99, nil
}
func (f *fakeMatchStore) UpdateMatch(_ context.Context, id int64, in store.MatchInput) error {
	f.updated = append(f.updated, in)
	return f.updateErr
}
func (f *fakeMatchStore) call(name string, id int64) error {
	f.calls = append(f.calls, fmt.Sprintf("%s:%d", name, id))
	return nil
}
func (f *fakeMatchStore) DeleteMatch(_ context.Context, id int64) error { return f.call("delete", id) }
func (f *fakeMatchStore) RetryMatch(_ context.Context, id int64) error  { return f.call("retry", id) }
func (f *fakeMatchStore) ResetMatch(_ context.Context, id int64) error  { return f.call("reset", id) }
func (f *fakeMatchStore) ArchiveMatch(_ context.Context, id int64) error {
	return f.call("archive", id)
}
func (f *fakeMatchStore) DuplicateMatch(_ context.Context, id int64) (int64, error) {
	return 77, f.call("duplicate", id)
}
func (f *fakeMatchStore) StartMatch(_ context.Context, id, serverID int64) error {
	return f.call(fmt.Sprintf("start-on-%d", serverID), id)
}
func (f *fakeMatchStore) AssignServer(_ context.Context, id, serverID int64) error {
	return f.call(fmt.Sprintf("assign-%d", serverID), id)
}
func (f *fakeMatchStore) ListMaps(_ context.Context, id int64) ([]store.Map, error) {
	return f.maps[id], nil
}
func (f *fakeMatchStore) ListMapScores(_ context.Context, mapID int64) ([]store.MapScore, error) {
	return f.scores[mapID], nil
}
func (f *fakeMatchStore) GetMapScore(_ context.Context, id int64) (*store.MapScore, error) {
	for _, list := range f.scores {
		for _, s := range list {
			if s.ID == id {
				return &s, nil
			}
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakeMatchStore) UpdateMapScore(_ context.Context, id int64, in store.MapScoreInput) error {
	return f.call(fmt.Sprintf("score-%d-%d", in.Score1Side1, in.Score2Side2), id)
}
func (f *fakeMatchStore) ListPlayers(context.Context, int64, int64) ([]store.Player, error) {
	return []store.Player{
		{ID: 1, MapID: 10, Team: "a", Nickname: "alice", Kills: 20, Deaths: 10, Headshots: 10},
		{ID: 2, MapID: 10, Team: "b", Nickname: "bob", Kills: 5, Deaths: 0},
	}, nil
}
func (f *fakeMatchStore) ListRoundSummaries(context.Context, int64, int64) ([]store.RoundSummary, error) {
	return []store.RoundSummary{{ID: 1, MapID: 10, RoundID: 3, TeamWin: "a", CTWin: true, ScoreA: 2, ScoreB: 1, WinType: "normal"}}, nil
}
func (f *fakeMatchStore) ListServers(context.Context) ([]store.Server, error) { return f.servers, nil }
func (f *fakeMatchStore) FreeServers(context.Context) ([]store.Server, error) { return f.servers, nil }
func (f *fakeMatchStore) GetServer(_ context.Context, id int64) (*store.Server, error) {
	for _, s := range f.servers {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakeMatchStore) ListTeams(context.Context) ([]store.Team, error) { return f.teams, nil }
func (f *fakeMatchStore) ListSeasons(context.Context) ([]store.Season, error) {
	return f.seasons, nil
}

type matchSentCommand struct {
	matchID                        int64
	action, arg, serverIP, authkey string
}

type fakeMatchSender struct {
	sent []matchSentCommand
	err  error
}

func (s *fakeMatchSender) Send(_ context.Context, id int64, action, arg, ip, key string) error {
	s.sent = append(s.sent, matchSentCommand{id, action, arg, ip, key})
	return s.err
}

func matchI64(v int64) *int64 { return &v }

func sampleMatch() store.Match {
	return store.Match{
		ID: 5, ServerID: matchI64(1), IP: "10.0.0.1:27015", ConfigAuthKey: "super-secret-key",
		TeamAName: "Alpha", TeamBName: "Bravo", TeamAFlag: "DK", TeamAID: matchI64(1), TeamBID: matchI64(2),
		Status: store.StatusFirstSide, Enable: true, ScoreA: 3, ScoreB: 2, MaxRound: 12, Rules: "rules",
		MapSelectionMode: "normal", CurrentMapID: matchI64(10), CurrentMap: &store.Map{ID: 10, Name: "de_dust2"},
		TVIP: "tv.example.com:27020",
	}
}

func matchTestConfig() *config.Config {
	return &config.Config{
		Title: "eBot", Mode: "net", DemoDownload: true, InsecureCookies: true,
		Maps:  []string{"de_dust2", "de_mirage", "de_inferno"},
		Rules: []string{"rules", "esl5on5"},
		Defaults: config.MatchDefaults{
			MaxRound: 12, Rules: "rules", OvertimeMaxRound: 3, OvertimeStartMoney: 10000,
		},
	}
}

type matchTestEnv struct {
	app    *App
	st     *fakeMatchStore
	sender *fakeMatchSender
	mux    http.Handler
	logs   *strings.Builder
}

func newMatchTestEnv(t *testing.T) *matchTestEnv {
	t.Helper()
	logs := &strings.Builder{}
	app := &App{Cfg: matchTestConfig(), Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	if err := NewApp(app, os.DirFS("../../web")); err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	m := sampleMatch()
	idle := store.Match{ID: 6, TeamAName: "Echo", TeamBName: "Foxtrot", Status: store.StatusNotStarted, MaxRound: 12, Rules: "rules", MapSelectionMode: "normal"}
	st := &fakeMatchStore{
		matches: []store.Match{m, idle},
		maps: map[int64][]store.Map{
			5: {{ID: 10, MatchID: 5, Name: "de_dust2", Score1: 3, Score2: 2, TVRecordFile: "demo1"}},
			6: {{ID: 11, MatchID: 6, Name: "de_mirage"}},
		},
		scores:  map[int64][]store.MapScore{10: {{ID: 100, MapID: 10, Type: "normal", Score1Side1: 3, Score2Side1: 2}}, 11: {{ID: 101, MapID: 11, Type: "normal"}}},
		servers: []store.Server{{ID: 1, IP: "10.0.0.1:27015", Hostname: "srv1"}, {ID: 2, IP: "10.0.0.2:27015"}},
		teams:   []store.Team{{ID: 1, Name: "Alpha", Flag: "DK"}, {ID: 2, Name: "Bravo", Flag: "SE"}},
		seasons: []store.Season{{ID: 1, Name: "Season 1", Active: true}, {ID: 2, Name: "Old", Active: false}},
	}
	sender := &fakeMatchSender{}
	h := &matchHandlers{app: app, st: st, cmd: sender}
	mux := http.NewServeMux()
	h.register(mux)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role := r.Header.Get("X-Test-Role"); role != "" {
			r = r.WithContext(auth.WithSession(r.Context(), auth.Session{Subject: "t", Name: "Tester", Role: role, Source: auth.SourceLocal}))
		}
		mux.ServeHTTP(w, r)
	})
	return &matchTestEnv{app: app, st: st, sender: sender, mux: handler, logs: logs}
}

func (e *matchTestEnv) do(method, target, role string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func TestMatchActions(t *testing.T) {
	labels := func(as []Action) []string {
		var out []string
		for _, a := range as {
			out = append(out, a.Label)
		}
		return out
	}
	cases := []struct {
		name   string
		status int
		enable bool
		want   []string
	}{
		{"not started", 0, false, []string{"Start", "Edit", "Delete", "Duplicate"}},
		{"starting", 1, true, []string{"Stop", "Stop with restart", "RCON and backups"}},
		{"warmup knife", 2, true, []string{"Stop", "Stop with restart", "RCON and backups", "Start knife", "Skip knife"}},
		{"knife", 3, true, []string{"Stop", "Stop with restart", "RCON and backups", "End knife"}},
		{"end knife", 4, true, []string{"Stop", "Stop with restart", "RCON and backups", "End knife"}},
		{"warmup first side", 5, true, []string{"Stop", "Stop with restart", "RCON and backups", "End warmup or halftime"}},
		{"first side", 6, true, []string{"Stop", "Stop with restart", "RCON and backups", "Stop to warmup", "Pause or unpause"}},
		{"warmup second side", 7, true, []string{"Stop", "Stop with restart", "RCON and backups", "End warmup or halftime"}},
		{"second side", 8, true, []string{"Stop", "Stop with restart", "RCON and backups", "Stop to warmup", "Pause or unpause"}},
		{"warmup ot first", 9, true, []string{"Stop", "Stop with restart", "RCON and backups", "End warmup or halftime"}},
		{"ot first", 10, true, []string{"Stop", "Stop with restart", "RCON and backups", "Stop to warmup", "Pause or unpause"}},
		{"warmup ot second", 11, true, []string{"Stop", "Stop with restart", "RCON and backups", "End warmup or halftime"}},
		{"ot second", 12, true, []string{"Stop", "Stop with restart", "RCON and backups", "Stop to warmup", "Pause or unpause"}},
		{"finished", 13, false, []string{"Archive", "Delete", "Duplicate"}},
		{"archived", 14, false, []string{"Delete", "Duplicate"}},
		{"stopped mid-game", 6, false, []string{"Restart", "Reset de_dust2", "Edit", "Delete", "Duplicate"}},
		{"stopped in warmup", 5, false, []string{"Restart", "Reset de_dust2", "Edit", "Delete", "Duplicate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleMatch()
			m.Status, m.Enable = tc.status, tc.enable
			got := matchActions(m)
			if !slices.Equal(labels(got), tc.want) {
				t.Fatalf("labels = %v, want %v", labels(got), tc.want)
			}
			for _, a := range got {
				if a.Command != "" && !ebotcmd.ValidAction(a.Command) {
					t.Errorf("invalid command %q", a.Command)
				}
				if strings.HasPrefix(a.Command, "skipmapprev") || strings.HasPrefix(a.Command, "skipmapnext") {
					t.Errorf("must not emit %q", a.Command)
				}
				set := 0
				for _, s := range []string{a.Command, a.Path, a.Href} {
					if s != "" {
						set++
					}
				}
				if set != 1 {
					t.Errorf("%q must have exactly one of command, path, href", a.Label)
				}
			}
		})
	}
}

func TestMatchActionsExtras(t *testing.T) {
	m := sampleMatch()
	m.MapSelectionMode = "bo3_modeb"
	m.ConfigStreamer = 1
	var cmds []string
	for _, a := range matchActions(m) {
		cmds = append(cmds, a.Command)
	}
	for _, want := range []string{"skipmap", "streamerready"} {
		if !slices.Contains(cmds, want) {
			t.Errorf("missing %s in %v", want, cmds)
		}
	}
	m.ConfigStreamer = 2
	for _, a := range matchActions(m) {
		if a.Command == "streamerready" && !a.Disabled {
			t.Error("streamer 2 must be disabled")
		}
	}
	m.ConfigStreamer = 0
	for _, a := range matchActions(m) {
		if a.Command == "streamerready" {
			t.Error("streamer 0 must not show the button")
		}
	}
}

func validMatchForm() url.Values {
	return url.Values{
		"season_id": {"1"}, "team_a": {"1"}, "team_b": {"2"},
		"rules": {"rules"}, "config_password": {"pw"}, "max_round": {"12"},
		"config_knife_round": {"1"}, "overtime_startmoney": {"10000"}, "overtime_max_round": {"3"},
		"startdate": {"21.10.2026 18:30"}, "auto_start_time": {"5"},
		"map_selection_mode": {"normal"}, "map": {"de_dust2"}, "server_id": {"1"},
	}
}

func parseMatchWith(t *testing.T, e *matchTestEnv, form url.Values, existing *store.Match) (store.MatchInput, map[string]string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/admin/matches", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	h := &matchHandlers{app: e.app, st: e.st}
	refs, err := h.refs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, in, errs := h.parseMatchForm(req, refs, existing)
	return in, errs
}

func TestParseMatchForm(t *testing.T) {
	e := newMatchTestEnv(t)

	t.Run("valid", func(t *testing.T) {
		in, errs := parseMatchWith(t, e, validMatchForm(), nil)
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if in.TeamAName != "Alpha" || in.TeamBFlag != "SE" || in.MaxRound != 12 || len(in.Maps) != 1 || in.Maps[0].Name != "de_dust2" {
			t.Errorf("unexpected input: %+v", in)
		}
		if in.ServerID == nil || *in.ServerID != 1 || in.SeasonID == nil || in.StartDate == nil {
			t.Errorf("server, season or start date missing: %+v", in)
		}
		if in.AutoStart {
			t.Error("autostart must be off when unchecked")
		}
	})

	bad := []struct {
		name  string
		edit  func(url.Values)
		field string
	}{
		{"bad map", func(v url.Values) { v.Set("map", "de_hacked") }, "map"},
		{"missing map", func(v url.Values) { v.Set("map", "") }, "map"},
		{"bad rules", func(v url.Values) { v.Set("rules", "../../etc") }, "rules"},
		{"no rules", func(v url.Values) { v.Set("rules", "") }, "rules"},
		{"missing team a", func(v url.Values) { v.Set("team_a", ""); v.Set("team_a_name", "") }, "team_a_name"},
		{"missing team b", func(v url.Values) { v.Set("team_b", ""); v.Set("team_b_name", "") }, "team_b_name"},
		{"unknown team", func(v url.Values) { v.Set("team_a", "999") }, "team_a"},
		{"same team", func(v url.Values) { v.Set("team_b", "1") }, "team_b"},
		{"max round zero", func(v url.Values) { v.Set("max_round", "0") }, "max_round"},
		{"max round negative", func(v url.Values) { v.Set("max_round", "-3") }, "max_round"},
		{"max round text", func(v url.Values) { v.Set("max_round", "abc") }, "max_round"},
		{"bad mode", func(v url.Values) { v.Set("map_selection_mode", "bo9") }, "map_selection_mode"},
		{"unknown season", func(v url.Values) { v.Set("season_id", "42") }, "season_id"},
		{"unknown server", func(v url.Values) { v.Set("server_id", "42") }, "server_id"},
		{"bad date", func(v url.Values) { v.Set("startdate", "tomorrow") }, "startdate"},
		{"bad flag", func(v url.Values) { v.Set("team_a", ""); v.Set("team_a_name", "X"); v.Set("team_a_flag", "<b>") }, "team_a_flag"},
		{"bad ot money", func(v url.Values) { v.Set("config_ot", "1"); v.Set("overtime_startmoney", "-5") }, "overtime_startmoney"},
		{"bad ot rounds", func(v url.Values) { v.Set("config_ot", "1"); v.Set("overtime_max_round", "4") }, "overtime_max_round"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			v := validMatchForm()
			tc.edit(v)
			_, errs := parseMatchWith(t, e, v, nil)
			if errs[tc.field] == "" {
				t.Fatalf("no error for %s, got %v", tc.field, errs)
			}
		})
	}

	t.Run("custom team", func(t *testing.T) {
		v := validMatchForm()
		v.Set("team_a", "")
		v.Set("team_a_name", "Pickup")
		v.Set("team_a_flag", "dk")
		in, errs := parseMatchWith(t, e, v, nil)
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		if in.TeamAID != nil || in.TeamAName != "Pickup" || in.TeamAFlag != "DK" {
			t.Errorf("unexpected: %+v", in)
		}
	})

	t.Run("tba map", func(t *testing.T) {
		v := validMatchForm()
		v.Set("map", "tba")
		in, errs := parseMatchWith(t, e, v, nil)
		if len(errs) != 0 || in.Maps[0].Name != "tba" {
			t.Fatalf("errs=%v maps=%v", errs, in.Maps)
		}
	})

	t.Run("autostart without date is off", func(t *testing.T) {
		v := validMatchForm()
		v.Set("auto_start", "1")
		v.Set("startdate", "")
		in, errs := parseMatchWith(t, e, v, nil)
		if len(errs) != 0 || in.AutoStart {
			t.Fatalf("errs=%v autostart=%v", errs, in.AutoStart)
		}
	})

	t.Run("bo3 still sends exactly one map", func(t *testing.T) {
		v := validMatchForm()
		v.Set("map_selection_mode", "bo3_modeb")
		v.Set("map2", "de_mirage")
		in, errs := parseMatchWith(t, e, v, nil)
		if len(errs) != 0 || len(in.Maps) != 1 {
			t.Fatalf("errs=%v maps=%v", errs, in.Maps)
		}
	})

	t.Run("side", func(t *testing.T) {
		for _, side := range []string{"", "ct", "t"} {
			v := validMatchForm()
			v.Set("side", side)
			in, errs := parseMatchWith(t, e, v, nil)
			if len(errs) != 0 || in.Side != side {
				t.Fatalf("side %q: errs=%v in=%q", side, errs, in.Side)
			}
		}
		v := validMatchForm()
		v.Set("side", "random")
		if _, errs := parseMatchWith(t, e, v, nil); errs["side"] == "" {
			t.Fatal("bad side accepted")
		}
	})

	t.Run("existing rules are kept", func(t *testing.T) {
		m := sampleMatch()
		m.Rules = "legacy"
		m.MaxRound = 7
		v := validMatchForm()
		v.Set("rules", "legacy")
		v.Set("max_round", "7")
		if _, errs := parseMatchWith(t, e, v, &m); len(errs) != 0 {
			t.Fatalf("errs=%v", errs)
		}
		if _, errs := parseMatchWith(t, e, v, nil); errs["rules"] == "" || errs["max_round"] == "" {
			t.Fatalf("legacy values must only pass for an existing match, got %v", errs)
		}
	})
}

func TestCreateAndEditHandlers(t *testing.T) {
	e := newMatchTestEnv(t)

	rec := e.do("POST", "/admin/matches", "admin", validMatchForm())
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/matches/new" {
		t.Fatalf("create: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if len(e.st.created) != 1 {
		t.Fatalf("created = %d", len(e.st.created))
	}

	bad := validMatchForm()
	bad.Set("map", "de_hacked")
	rec = e.do("POST", "/admin/matches", "admin", bad)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid create status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Choose a map from the list.") {
		t.Error("field error not rendered")
	}
	if len(e.st.created) != 1 {
		t.Error("an invalid form must not create a match")
	}

	rec = e.do("POST", "/admin/matches/6", "admin", validMatchForm())
	if rec.Code != http.StatusSeeOther || len(e.st.updated) != 1 {
		t.Fatalf("update: %d updated=%d", rec.Code, len(e.st.updated))
	}

	e.st.updateErr = store.ErrMatchEnabled
	rec = e.do("POST", "/admin/matches/6", "admin", validMatchForm())
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Set-Cookie"), "ebot_flash") {
		t.Fatalf("enabled refusal: %d %v", rec.Code, rec.Header())
	}

	rec = e.do("POST", "/admin/matches/5", "admin", validMatchForm())
	if rec.Code != http.StatusSeeOther || len(e.st.updated) != 2 {
		t.Fatalf("an enabled match must not be updated: %d", rec.Code)
	}

	rec = e.do("GET", "/admin/matches/5/edit", "admin", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("edit of an enabled match = %d", rec.Code)
	}
	rec = e.do("GET", "/admin/matches/6/edit", "admin", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="map"`) {
		t.Fatalf("edit form = %d", rec.Code)
	}
	rec = e.do("GET", "/admin/matches/new", "viewer", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="max_round"`) {
		t.Fatalf("new form = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), ">Old<") {
		t.Error("inactive seasons must not be offered on a new match")
	}
}

func TestMatchCommandEndpoint(t *testing.T) {
	post := func(e *matchTestEnv, id string, form url.Values) (int, commandReply) {
		rec := e.do("POST", "/admin/matches/"+id+"/command", "admin", form)
		var rep commandReply
		if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
			t.Fatalf("bad json %q: %v", rec.Body.String(), err)
		}
		return rec.Code, rep
	}

	t.Run("sends", func(t *testing.T) {
		e := newMatchTestEnv(t)
		code, rep := post(e, "5", url.Values{"action": {"executeCommand"}, "arg": {"say hi"}})
		if code != 200 || !rep.OK {
			t.Fatalf("%d %+v", code, rep)
		}
		want := matchSentCommand{5, "executeCommand", "say hi", "10.0.0.1:27015", "super-secret-key"}
		if len(e.sender.sent) != 1 || e.sender.sent[0] != want {
			t.Fatalf("sent = %+v", e.sender.sent)
		}
		if strings.Contains(e.logs.String(), "super-secret-key") || strings.Contains(e.logs.String(), "say hi") {
			t.Errorf("log leaks secrets: %s", e.logs.String())
		}
	})

	t.Run("arg ignored for plain actions", func(t *testing.T) {
		e := newMatchTestEnv(t)
		post(e, "5", url.Values{"action": {"pauseunpause"}, "arg": {"rcon_password x"}})
		if len(e.sender.sent) != 1 || e.sender.sent[0].arg != "" {
			t.Fatalf("sent = %+v", e.sender.sent)
		}
	})

	t.Run("unknown action", func(t *testing.T) {
		e := newMatchTestEnv(t)
		for _, a := range []string{"", "rm -rf", "skipmapnext", "skipmapprev"} {
			code, rep := post(e, "5", url.Values{"action": {a}})
			if code != 400 || rep.OK {
				t.Errorf("%q: %d %+v", a, code, rep)
			}
		}
		if len(e.sender.sent) != 0 {
			t.Error("nothing may be sent")
		}
	})

	t.Run("bad args", func(t *testing.T) {
		e := newMatchTestEnv(t)
		for _, f := range []url.Values{
			{"action": {"executeCommand"}},
			{"action": {"executeCommand"}, "arg": {"a\nb"}},
			{"action": {"goBackRounds"}, "arg": {"x"}},
		} {
			if code, _ := post(e, "5", f); code != 400 {
				t.Errorf("%v: %d", f, code)
			}
		}
	})

	t.Run("missing authkey", func(t *testing.T) {
		e := newMatchTestEnv(t)
		e.st.matches[0].ConfigAuthKey = ""
		if code, rep := post(e, "5", url.Values{"action": {"stop"}}); code != 400 || rep.OK {
			t.Fatalf("%d %+v", code, rep)
		}
		if len(e.sender.sent) != 0 {
			t.Error("nothing may be sent")
		}
	})

	t.Run("missing ip", func(t *testing.T) {
		e := newMatchTestEnv(t)
		e.st.matches[0].IP = ""
		if code, _ := post(e, "5", url.Values{"action": {"stop"}}); code != 400 {
			t.Fatal(code)
		}
	})

	t.Run("hostname server", func(t *testing.T) {
		e := newMatchTestEnv(t)
		e.st.matches[0].IP = "game.example.com:27015"
		if code, _ := post(e, "5", url.Values{"action": {"stop"}}); code != 400 {
			t.Fatal(code)
		}
		rec := e.do("GET", "/matches/5", "admin", nil)
		if !strings.Contains(rec.Body.String(), "added by hostname") {
			t.Error("warning not shown")
		}
	})

	t.Run("unknown match", func(t *testing.T) {
		e := newMatchTestEnv(t)
		if code, _ := post(e, "404", url.Values{"action": {"stop"}}); code != 404 {
			t.Fatal(code)
		}
	})

	t.Run("send failure", func(t *testing.T) {
		e := newMatchTestEnv(t)
		e.sender.err = errors.New("redis: connection refused 10.1.1.1")
		code, rep := post(e, "5", url.Values{"action": {"stop"}})
		if code != 502 || rep.OK || strings.Contains(rep.Error, "redis") {
			t.Fatalf("%d %+v", code, rep)
		}
	})
}

func TestMatchAccessControl(t *testing.T) {
	e := newMatchTestEnv(t)
	posts := []string{
		"/admin/matches", "/admin/matches/6", "/admin/matches/6/start", "/admin/matches/6/assign-server",
		"/admin/matches/6/retry", "/admin/matches/6/reset", "/admin/matches/6/archive",
		"/admin/matches/6/duplicate", "/admin/matches/6/delete", "/admin/matches/5/command",
		"/admin/matches/5/scores/100",
	}
	for _, p := range posts {
		if rec := e.do("POST", p, "", url.Values{}); rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous POST %s = %d", p, rec.Code)
		}
		if rec := e.do("POST", p, auth.RoleViewer, url.Values{"action": {"stop"}}); rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST %s = %d", p, rec.Code)
		}
	}
	if len(e.st.calls) != 0 || len(e.sender.sent) != 0 || len(e.st.created) != 0 {
		t.Fatal("denied requests must not reach the store")
	}

	for _, p := range []string{"/admin/matches/new", "/admin/matches/6/edit", "/admin/matches/6/scores"} {
		rec := e.do("GET", p, "", nil)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("anonymous GET %s = %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
		if rec := e.do("GET", p, auth.RoleViewer, nil); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d", p, rec.Code)
		}
	}

	if rec := e.do("POST", "/admin/matches/6/retry", "admin", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Errorf("admin retry = %d", rec.Code)
	}
	if !slices.Contains(e.st.calls, "retry:6") {
		t.Errorf("calls = %v", e.st.calls)
	}
}

func TestAdminControlsOnlyForAdmins(t *testing.T) {
	e := newMatchTestEnv(t)
	for role, wantControls := range map[string]bool{"": false, "viewer": false, "admin": true} {
		body := e.do("GET", "/matches/5", role, nil).Body.String()
		for _, marker := range []string{`data-command="stopNoRs"`, `data-console="5"`, `data-rcon-form="5"`, `data-logger="5"`, "super-secret-key"} {
			has := strings.Contains(body, marker)
			if marker == "super-secret-key" {
				if has {
					t.Errorf("role %q: auth key leaked", role)
				}
				continue
			}
			if has != wantControls {
				t.Errorf("role %q: %s present = %v", role, marker, has)
			}
		}
		list := e.do("GET", "/matches", role, nil).Body.String()
		if has := strings.Contains(list, `data-command="stopNoRs"`); has != wantControls {
			t.Errorf("role %q: list controls = %v", role, has)
		}
	}
}

func TestMatchPagesRenderInLayout(t *testing.T) {
	e := newMatchTestEnv(t)

	rec := e.do("GET", "/matches?status=archived&page=2&season=1&team=2&q=al", "", nil)
	if rec.Code != 200 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`<main class="content">`, `data-live="matches"`, "data-live-reload", `data-match-id="5"`, `data-match-status="5"`, `data-live-state`, "/static/live.js", "Alpha", "Bravo"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
	f := e.st.filter
	if !f.Archived || f.Page != 2 || f.SeasonID != 1 || f.TeamID != 2 || f.Search != "al" || f.PerPage != 12 {
		t.Errorf("filter = %+v", f)
	}

	rec = e.do("GET", "/", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-live="matches"`) || e.st.filter.PerPage != 10 {
		t.Fatalf("home = %d filter=%+v", rec.Code, e.st.filter)
	}

	rec = e.do("GET", "/matches/5", "", nil)
	if rec.Code != 200 {
		t.Fatalf("view = %d %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	for _, want := range []string{"Match #5", "de_dust2", "/matches/5/demo/10", "alice", "Current map", "tv.example.com:27020"} {
		if !strings.Contains(body, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(body, "10.0.0.1:27015") {
		t.Error("net mode must show the GOTV address, not the server address")
	}

	e.app.Cfg.Mode = "lan"
	e.app.Cfg.DemoDownload = false
	body = e.do("GET", "/matches/5", "", nil).Body.String()
	if !strings.Contains(body, "10.0.0.1:27015") || strings.Contains(body, "/demo/") {
		t.Error("lan mode and demo toggle not honoured")
	}

	rec = e.do("GET", "/admin/matches/6/scores", "admin", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/admin/matches/6/scores/101") {
		t.Fatalf("scores = %d", rec.Code)
	}

	if rec := e.do("GET", "/matches/999", "", nil); rec.Code != 404 {
		t.Errorf("unknown match = %d", rec.Code)
	}
	if rec := e.do("GET", "/matches/abc", "", nil); rec.Code != 404 {
		t.Errorf("bad id = %d", rec.Code)
	}
}

func TestMatchStartMatch(t *testing.T) {
	e := newMatchTestEnv(t)
	e.do("POST", "/admin/matches/6/start", "admin", url.Values{"server_id": {"2"}})
	if !slices.Contains(e.st.calls, "start-on-2:6") {
		t.Fatalf("calls = %v", e.st.calls)
	}
	e.st.calls = nil
	e.do("POST", "/admin/matches/6/start", "admin", url.Values{})
	if len(e.st.calls) != 1 || !strings.HasPrefix(e.st.calls[0], "start-on-") {
		t.Fatalf("a free server must be picked: %v", e.st.calls)
	}
	e.st.calls = nil
	e.do("POST", "/admin/matches/5/start", "admin", url.Values{})
	if len(e.st.calls) != 0 {
		t.Fatal("a running match must not be started again")
	}
	e.do("POST", "/admin/matches/6/start", "admin", url.Values{"server_id": {"77"}})
	if len(e.st.calls) != 0 {
		t.Fatal("unknown server must be refused")
	}
}

func TestMatchScoreUpdate(t *testing.T) {
	e := newMatchTestEnv(t)
	e.do("POST", "/admin/matches/6/scores/101", "admin", url.Values{"score1_side1": {"7"}, "score1_side2": {"1"}, "score2_side1": {"2"}, "score2_side2": {"5"}})
	if !slices.Contains(e.st.calls, "score-7-5:101") {
		t.Fatalf("calls = %v", e.st.calls)
	}
	e.st.calls = nil
	e.do("POST", "/admin/matches/6/scores/101", "admin", url.Values{"score1_side1": {"-1"}, "score1_side2": {"1"}, "score2_side1": {"2"}, "score2_side2": {"5"}})
	e.do("POST", "/admin/matches/6/scores/100", "admin", url.Values{"score1_side1": {"1"}, "score1_side2": {"1"}, "score2_side1": {"2"}, "score2_side2": {"5"}})
	e.do("POST", "/admin/matches/5/scores/100", "admin", url.Values{"score1_side1": {"1"}, "score1_side2": {"1"}, "score2_side1": {"2"}, "score2_side2": {"5"}})
	if len(e.st.calls) != 0 {
		t.Fatalf("invalid, foreign or running-match scores must be refused: %v", e.st.calls)
	}
}
