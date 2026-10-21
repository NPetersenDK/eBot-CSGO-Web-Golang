package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type fakeStore struct {
	mu       sync.Mutex
	matches  map[int64]*store.Match
	servers  []store.Server
	nextID   int64
	started  []int64
	reset    []int64
	archived []int64
}

func newFakeStore() *fakeStore {
	sid := int64(7)
	return &fakeStore{
		matches: map[int64]*store.Match{
			1: {ID: 1, Status: store.StatusFirstSide, Enable: true, ServerID: &sid, IP: "1.2.3.4:27015", ConfigAuthKey: "secret", ConfigPassword: "pw"},
			2: {ID: 2, Status: store.StatusNotStarted},
			3: {ID: 3, Status: store.StatusFirstSide, Enable: false, ServerID: &sid, IP: "1.2.3.4:27015"},
			4: {ID: 4, Status: store.StatusEndMatch},
		},
		servers: []store.Server{{ID: 7, IP: "1.2.3.4:27015", RCON: "r", Hostname: "srv"}},
		nextID:  10,
	}
}

func (f *fakeStore) get(id int64) (*store.Match, error) {
	m, ok := f.matches[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := *m
	return &c, nil
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) ListMatches(_ context.Context, fl store.MatchFilter) ([]store.Match, store.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Match
	for _, m := range f.matches {
		if (m.Status == store.StatusArchived) == fl.Archived {
			out = append(out, *m)
		}
	}
	return out, store.Page{Page: 1, PerPage: fl.PerPage, Total: len(out)}, nil
}
func (f *fakeStore) GetMatch(_ context.Context, id int64) (*store.Match, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.get(id)
}
func (f *fakeStore) CreateMatch(_ context.Context, in store.MatchInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.matches[f.nextID] = &store.Match{ID: f.nextID, Rules: in.Rules, TeamAName: in.TeamAName, MaxRound: in.MaxRound}
	return f.nextID, nil
}
func (f *fakeStore) DeleteMatch(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.matches[id]; !ok {
		return store.ErrNotFound
	}
	if f.matches[id].Enable {
		return store.ErrMatchEnabled
	}
	delete(f.matches, id)
	return nil
}
func (f *fakeStore) StartMatch(_ context.Context, id, serverID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.matches[id]
	if !ok {
		return store.ErrNotFound
	}
	m.Enable, m.Status, m.ServerID = true, store.StatusStarting, &serverID
	f.started = append(f.started, serverID)
	return nil
}
func (f *fakeStore) AssignServer(_ context.Context, id, serverID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.matches[id].ServerID = &serverID
	return nil
}
func (f *fakeStore) ResetMatch(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.matches[id].Status = store.StatusNotStarted
	f.reset = append(f.reset, id)
	return nil
}
func (f *fakeStore) ArchiveMatch(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.matches[id]
	if !ok {
		return store.ErrNotFound
	}
	m.Status, m.Enable = store.StatusArchived, false
	f.archived = append(f.archived, id)
	return nil
}
func (f *fakeStore) ListMaps(context.Context, int64) ([]store.Map, error) {
	return []store.Map{{ID: 1, MatchID: 1, Name: "de_mirage", Score1: 5, Score2: 3, PickedBy: "default"}}, nil
}
func (f *fakeStore) ListPlayers(_ context.Context, _, mapID int64) ([]store.Player, error) {
	return []store.Player{{ID: 1, MapID: mapID, Nickname: "nick", Rounds2K: 4}}, nil
}
func (f *fakeStore) ListRoundSummaries(context.Context, int64, int64) ([]store.RoundSummary, error) {
	return []store.RoundSummary{{ID: 1, RoundID: 1, WinType: "normal"}}, nil
}
func (f *fakeStore) ListServers(context.Context) ([]store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Server(nil), f.servers...), nil
}
func (f *fakeStore) FreeServers(context.Context) ([]store.Server, error) {
	return f.ListServers(context.Background())
}
func (f *fakeStore) GetServer(_ context.Context, id int64) (*store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].ID == id {
			c := f.servers[i]
			return &c, nil
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) CreateServers(_ context.Context, in store.ServerCreate) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.servers = append(f.servers, store.Server{ID: f.nextID, IP: in.Host + ":" + itoa(in.Port), RCON: in.RCON, Hostname: in.Hostname})
	return 1, nil
}
func (f *fakeStore) UpdateServer(_ context.Context, id int64, in store.ServerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].ID == id {
			f.servers[i].IP, f.servers[i].Hostname = in.IP, in.Hostname
			return nil
		}
	}
	return store.ErrNotFound
}
func (f *fakeStore) DeleteServer(_ context.Context, id int64) error {
	return store.ErrNotFound
}
func (f *fakeStore) ListTeams(context.Context) ([]store.Team, error) {
	return []store.Team{{ID: 1, Name: "B", ShortName: "b"}, {ID: 2, Name: "A", ShortName: "a"}}, nil
}
func (f *fakeStore) GetTeam(_ context.Context, id int64) (*store.Team, error) {
	if id == 1 {
		return &store.Team{ID: 1, Name: "Alpha", Flag: "DK"}, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) ListSeasons(context.Context) ([]store.Season, error) {
	return []store.Season{{ID: 1, Name: "S1", Start: "2024-01-02"}}, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

type sentCmd struct {
	id                       int64
	action, arg, ip, authkey string
}

type fakeCmd struct {
	f    *fakeStore
	sent []sentCmd
	err  error
	// release clears enable on the match, as the bot does.
	release bool
}

func (c *fakeCmd) Send(_ context.Context, id int64, action, arg, ip, authkey string) error {
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, sentCmd{id, action, arg, ip, authkey})
	if c.release {
		c.f.mu.Lock()
		c.f.matches[id].Enable = false
		c.f.mu.Unlock()
	}
	return nil
}

type env struct {
	h   http.Handler
	f   *fakeStore
	cmd *fakeCmd
}

func newEnv(t *testing.T, withCmd bool) *env {
	t.Helper()
	f := newFakeStore()
	d := deps{store: f, keys: []string{"devkey", "other"}, stopTimeout: 300 * time.Millisecond, stopPoll: 5 * time.Millisecond}
	e := &env{f: f}
	if withCmd {
		e.cmd = &fakeCmd{f: f, release: true}
		d.cmd = e.cmd
	}
	mux := http.NewServeMux()
	newServer(d).routes(mux)
	e.h = mux
	return e
}

func (e *env) do(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-API-Key", "devkey")
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func want(t *testing.T, rr *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rr.Code != code {
		t.Fatalf("status = %d, want %d, body %s", rr.Code, code, rr.Body.String())
	}
}

func TestOpenAPISpecServed(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/openapi.json", "", "X-API-Key", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"openapi"`) {
		t.Fatalf("not an OpenAPI doc: %.80s", rr.Body.String())
	}
}

func TestHealthPublic(t *testing.T) {
	e := newEnv(t, false)
	want(t, e.do("GET", "/api/health", "", "X-API-Key", ""), 200)
}

func TestAuth(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/matches", "", "X-API-Key", "")
	want(t, rr, 401)
	if !strings.Contains(rr.Body.String(), `"error"`) || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("401 must be JSON: %s", rr.Body.String())
	}
	want(t, e.do("GET", "/api/matches", "", "X-API-Key", "nope"), 401)
	want(t, e.do("GET", "/api/matches", "", "X-API-Key", "dev"), 401)
	want(t, e.do("GET", "/api/matches", "", "X-API-Key", "other"), 200)
	want(t, e.do("GET", "/api/matches?api_key=devkey", "", "X-API-Key", ""), 401)
	if e.do("GET", "/api/matches", "").Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS expected")
	}
}

func TestNoKeysNoRoutes(t *testing.T) {
	mux := http.NewServeMux()
	newServer(deps{store: newFakeStore()}).routes(mux)
	for _, p := range []string{"/api/matches", "/api/health", "/api/openapi.json"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("GET", p, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", p, rr.Code)
		}
	}
}

func TestGetMatch(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/matches/1", "")
	want(t, rr, 200)
	body := rr.Body.String()
	if !strings.Contains(body, `"status_text":"First side"`) || !strings.Contains(body, `"config_password":"pw"`) {
		t.Fatalf("unexpected body %s", body)
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "authkey") {
		t.Fatalf("authkey leaked: %s", body)
	}
	want(t, e.do("GET", "/api/matches/99", ""), 404)
	want(t, e.do("GET", "/api/matches/abc", ""), 400)
}

func TestListMatchesStatusFilter(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/matches?status=0", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"id":2`) || strings.Contains(rr.Body.String(), `"id":1,`) {
		t.Fatalf("unexpected %s", rr.Body.String())
	}
	want(t, e.do("GET", "/api/matches?status=x", ""), 400)
}

func TestCreateMatch(t *testing.T) {
	e := newEnv(t, false)
	want(t, e.do("POST", "/api/matches", `{"rules":"x"}`, "Content-Type", "text/plain"), 415)
	want(t, e.do("POST", "/api/matches", `{}`), 400)
	want(t, e.do("POST", "/api/matches", `{"rules":"x","bogus":1}`), 400)
	want(t, e.do("POST", "/api/matches", `{"rules":"x","map_selection_mode":"zzz"}`), 400)
	want(t, e.do("POST", "/api/matches", `{"rules":"x","server_id":99}`), 400)
	want(t, e.do("POST", "/api/matches", `{"rules":"`+strings.Repeat("a", 2<<20)+`"}`), 413)

	rr := e.do("POST", "/api/matches", `{"rules":"x","team_a_id":1,"start":true}`)
	want(t, rr, 201)
	if !strings.HasPrefix(rr.Header().Get("Location"), "/api/matches/") {
		t.Fatalf("location %q", rr.Header().Get("Location"))
	}
	if !strings.Contains(rr.Body.String(), `"team_a_name":"Alpha"`) || !strings.Contains(rr.Body.String(), `"enable":true`) {
		t.Fatalf("unexpected: %s", rr.Body.String())
	}
	if len(e.f.started) != 1 || e.f.started[0] != 7 {
		t.Fatalf("started = %v", e.f.started)
	}
}

func TestStartArchiveDelete(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("POST", "/api/matches/2/start", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"enable":true`) || len(e.f.started) != 1 {
		t.Fatalf("not started: %s", rr.Body.String())
	}
	want(t, e.do("POST", "/api/matches/99/start", ""), 404)
	want(t, e.do("POST", "/api/matches/4/archive", ""), 200)
	want(t, e.do("POST", "/api/matches/99/archive", ""), 404)
	want(t, e.do("DELETE", "/api/matches/1", ""), 409)
	want(t, e.do("DELETE", "/api/matches/4", ""), 204)
	want(t, e.do("DELETE", "/api/matches/4", ""), 404)
}

func TestStop(t *testing.T) {
	e := newEnv(t, true)
	want(t, e.do("POST", "/api/matches/1/stop", ""), 200)
	if len(e.cmd.sent) != 1 || e.cmd.sent[0] != (sentCmd{1, "stop", "", "1.2.3.4:27015", "secret"}) {
		t.Fatalf("sent = %+v", e.cmd.sent)
	}

	e = newEnv(t, true)
	want(t, e.do("POST", "/api/matches/1/stop?restart=false", ""), 200)
	if e.cmd.sent[0].action != "stopNoRs" {
		t.Fatalf("action = %s", e.cmd.sent[0].action)
	}

	e = newEnv(t, true)
	want(t, e.do("POST", "/api/matches/3/stop", ""), 409)
	want(t, e.do("POST", "/api/matches/99/stop", ""), 404)
	if len(e.cmd.sent) != 0 {
		t.Fatal("nothing should be sent")
	}

	e = newEnv(t, true)
	e.cmd.release = false
	want(t, e.do("POST", "/api/matches/1/stop", ""), 504)

	e = newEnv(t, true)
	e.cmd.err = errors.New("redis down")
	want(t, e.do("POST", "/api/matches/1/stop", ""), 500)

	want(t, newEnv(t, false).do("POST", "/api/matches/1/stop", ""), 503)
}

func TestReset(t *testing.T) {
	e := newEnv(t, false)
	want(t, e.do("POST", "/api/matches/3/reset", ""), 200)
	if len(e.f.reset) != 1 {
		t.Fatal("reset not called")
	}
	want(t, e.do("POST", "/api/matches/1/reset", ""), 409)
	want(t, e.do("POST", "/api/matches/2/reset", ""), 409)
	want(t, e.do("POST", "/api/matches/4/reset", ""), 409)
	want(t, e.do("POST", "/api/matches/99/reset", ""), 404)
}

func TestRestart(t *testing.T) {
	e := newEnv(t, true)
	rr := e.do("POST", "/api/matches/1/restart", "")
	want(t, rr, 200)
	if len(e.cmd.sent) != 1 || e.cmd.sent[0].action != "stop" {
		t.Fatalf("sent = %+v", e.cmd.sent)
	}
	if len(e.f.reset) != 1 || len(e.f.started) != 1 || e.f.started[0] != 7 {
		t.Fatalf("reset=%v started=%v", e.f.reset, e.f.started)
	}

	e = newEnv(t, true)
	want(t, e.do("POST", "/api/matches/3/restart", ""), 200)
	if len(e.cmd.sent) != 0 || len(e.f.reset) != 1 {
		t.Fatalf("sent=%v reset=%v", e.cmd.sent, e.f.reset)
	}
	want(t, e.do("POST", "/api/matches/2/restart", ""), 409)
	want(t, e.do("POST", "/api/matches/99/restart", ""), 404)
	want(t, newEnv(t, false).do("POST", "/api/matches/3/restart", ""), 503)
}

func TestMatchData(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/matches/1/maps", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"rounds_played":8`) {
		t.Fatalf("%s", rr.Body.String())
	}
	rr = e.do("GET", "/api/matches/1/players?map_id=5", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"pseudo":"nick"`) || !strings.Contains(rr.Body.String(), `"nb2":4`) {
		t.Fatalf("%s", rr.Body.String())
	}
	want(t, e.do("GET", "/api/matches/1/players?map_id=x", ""), 400)
	want(t, e.do("GET", "/api/matches/1/rounds", ""), 200)
	want(t, e.do("GET", "/api/matches/1/kills", ""), 501)
}

func TestServers(t *testing.T) {
	e := newEnv(t, false)
	want(t, e.do("GET", "/api/servers", ""), 200)
	want(t, e.do("GET", "/api/servers/7", ""), 200)
	want(t, e.do("GET", "/api/servers/8", ""), 404)
	want(t, e.do("POST", "/api/servers", `{"ip":"1.1.1.1:1","rcon":"","hostname":"h"}`), 400)
	want(t, e.do("POST", "/api/servers", `{"ip":"nope","rcon":"r","hostname":"h"}`), 400)
	rr := e.do("POST", "/api/servers", `{"ip":"1.1.1.1:27016","rcon":"r","hostname":"h"}`)
	want(t, rr, 201)
	if !strings.Contains(rr.Body.String(), `"ip":"1.1.1.1:27016"`) {
		t.Fatalf("%s", rr.Body.String())
	}
	want(t, e.do("PUT", "/api/servers/7", `{"ip":"1.1.1.1:1","rcon":"r","hostname":"h2"}`), 200)
	want(t, e.do("PUT", "/api/servers/70", `{"ip":"1.1.1.1:1","rcon":"r","hostname":"h2"}`), 404)
	want(t, e.do("DELETE", "/api/servers/70", ""), 404)
}

func TestTeamsSeasons(t *testing.T) {
	e := newEnv(t, false)
	rr := e.do("GET", "/api/teams", "")
	want(t, rr, 200)
	if !strings.HasPrefix(rr.Body.String(), `[{"id":2,"name":"A"`) {
		t.Fatalf("not sorted by name: %s", rr.Body.String())
	}
	rr = e.do("GET", "/api/seasons", "")
	want(t, rr, 200)
	if !strings.Contains(rr.Body.String(), `"start":"2024-01-02T00:00:00Z"`) {
		t.Fatalf("%s", rr.Body.String())
	}
}

func TestUnsupportedStoreIs501(t *testing.T) {
	rr := httptest.NewRecorder()
	storeError(rr, errors.ErrUnsupported, "x")
	want(t, rr, 501)
}
