package web

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type fakeStore struct {
	crudStore
	mu      sync.Mutex
	next    int64
	servers map[int64]store.Server
	seasons map[int64]store.Season
	users   map[int64]store.User
	maps    map[int64]store.Map
	created []store.ServerCreate
}

func newFake() *fakeStore {
	return &fakeStore{
		next:    100,
		servers: map[int64]store.Server{},
		seasons: map[int64]store.Season{},
		users:   map[int64]store.User{},
		maps:    map[int64]store.Map{},
	}
}

func (f *fakeStore) id() int64 { f.next++; return f.next }

func (f *fakeStore) Counts(context.Context) (store.Counts, error) {
	return store.Counts{Matches: 7, Servers: len(f.servers)}, nil
}

func (f *fakeStore) ListServers(context.Context) ([]store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Server
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStore) GetServer(_ context.Context, id int64) (*store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &s, nil
}

func (f *fakeStore) CreateServers(_ context.Context, in store.ServerCreate) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	n := 1
	if in.Mode == "range" {
		n = in.PortEnd - in.Port + 1
	}
	for i := 0; i < n; i++ {
		id := f.id()
		f.servers[id] = store.Server{ID: id, IP: in.Host + ":" + strconv.Itoa(in.Port+i), RCON: in.RCON, Hostname: in.Hostname}
	}
	return n, nil
}

func (f *fakeStore) UpdateServer(_ context.Context, id int64, in store.ServerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[id]
	if !ok {
		return store.ErrNotFound
	}
	s.IP, s.RCON, s.Hostname, s.TVIP = in.IP, in.RCON, in.Hostname, in.TVIP
	f.servers[id] = s
	return nil
}

func (f *fakeStore) DeleteServer(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[id]
	if !ok {
		return store.ErrNotFound
	}
	if s.InUse {
		return store.ErrServerBusy
	}
	delete(f.servers, id)
	return nil
}

func (f *fakeStore) ListSeasons(context.Context) ([]store.Season, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Season
	for _, s := range f.seasons {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStore) GetSeason(_ context.Context, id int64) (*store.Season, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.seasons[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &s, nil
}

func (f *fakeStore) CreateSeason(_ context.Context, in store.SeasonInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.id()
	f.seasons[id] = store.Season{ID: id, Name: in.Name, Event: in.Event, Start: in.Start, End: in.End, Link: in.Link, Logo: in.Logo, Active: in.Active}
	return id, nil
}

func (f *fakeStore) UpdateSeason(_ context.Context, id int64, in store.SeasonInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.seasons[id]; !ok {
		return store.ErrNotFound
	}
	f.seasons[id] = store.Season{ID: id, Name: in.Name, Event: in.Event, Start: in.Start, End: in.End, Link: in.Link, Logo: in.Logo, Active: in.Active}
	return nil
}

func (f *fakeStore) DeleteSeason(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.seasons[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.seasons, id)
	return nil
}

func (f *fakeStore) DeactivateSeason(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.seasons[id]
	if !ok {
		return store.ErrNotFound
	}
	s.Active = false
	f.seasons[id] = s
	return nil
}

func (f *fakeStore) ListUsers(context.Context) ([]store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.User
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeStore) GetUser(_ context.Context, id int64) (*store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &u, nil
}

func (f *fakeStore) dup(in store.UserInput, self int64) bool {
	for _, u := range f.users {
		if u.ID != self && (strings.EqualFold(u.Username, in.Username) || strings.EqualFold(u.Email, in.Email)) {
			return true
		}
	}
	return false
}

func (f *fakeStore) CreateUser(_ context.Context, in store.UserInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dup(in, 0) {
		return 0, store.ErrDuplicate
	}
	id := f.id()
	f.users[id] = store.User{ID: id, Username: in.Username, Email: in.Email, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin, CreatedAt: time.Now()}
	return id, nil
}

func (f *fakeStore) UpdateUser(_ context.Context, id int64, in store.UserInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dup(in, id) {
		return store.ErrDuplicate
	}
	f.users[id] = store.User{ID: id, Username: in.Username, Email: in.Email, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin}
	return nil
}

func (f *fakeStore) DeleteUser(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, id)
	return nil
}

func (f *fakeStore) ListTeams(context.Context) ([]store.Team, error) { return nil, nil }
func (f *fakeStore) ListAds(context.Context) ([]store.Ad, error)     { return nil, nil }

func (f *fakeStore) GetMap(_ context.Context, id int64) (*store.Map, error) {
	m, ok := f.maps[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &m, nil
}

type testEnv struct {
	app *App
	srv http.Handler
	st  *fakeStore
}

// newEnv builds a real App for rendering and mounts the handlers on a fake
// store. The role comes from the X-Role header and the user from X-User.
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := &config.Config{
		Title:             "eBot",
		InsecureCookies:   true,
		DemoPath:          t.TempDir(),
		DemoDownload:      true,
		LogMatchAdminPath: t.TempDir(),
	}
	app := &App{Cfg: cfg}
	if err := NewApp(app, os.DirFS("../../web")); err != nil {
		t.Fatal(err)
	}
	st := newFake()
	mux := http.NewServeMux()
	app.mountCrud(mux, st)
	app.mountFiles(mux, st)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role := r.Header.Get("X-Role"); role != "" {
			user := r.Header.Get("X-User")
			if user == "" {
				user = "tester"
			}
			r = r.WithContext(auth.WithSession(r.Context(), auth.Session{Subject: user, Name: user, Role: role, Source: auth.SourceLocal}))
		}
		mux.ServeHTTP(w, r)
	})
	return &testEnv{app: app, srv: h, st: st}
}

func (e *testEnv) do(method, path, role string, form url.Values) *httptest.ResponseRecorder {
	return e.doAs(method, path, role, "", form)
}

func (e *testEnv) doAs(method, path, role, user string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if role != "" {
		r.Header.Set("X-Role", role)
		r.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, r)
	return w
}

func flashOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == flashCookie {
			b, err := base64.RawURLEncoding.DecodeString(strings.SplitN(c.Value, ".", 2)[0])
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	return ""
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status = %d, want %d; body: %.300s", w.Code, code, w.Body.String())
	}
}

func TestServerCRUD(t *testing.T) {
	e := newEnv(t)

	w := e.do("GET", "/admin/servers/new", "admin", nil)
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "cannot be controlled by the bot") {
		t.Error("hostname hint missing")
	}

	w = e.do("POST", "/admin/servers", "admin", url.Values{"mode": {"single"}, "host": {"not an ip"}, "port": {"27015"}, "hostname": {"a"}, "rcon": {"x"}})
	wantStatus(t, w, 422)
	if !strings.Contains(w.Body.String(), "Enter a valid IPv4 address.") {
		t.Errorf("no field error: %s", w.Body.String())
	}
	if len(e.st.servers) != 0 {
		t.Fatal("invalid form stored a server")
	}

	w = e.do("POST", "/admin/servers", "admin", url.Values{"mode": {"range"}, "host": {"10.0.0.5"}, "port": {"27015"}, "port_end": {"27014"}, "hostname": {"a"}, "rcon": {"x"}})
	wantStatus(t, w, 422)

	w = e.do("POST", "/admin/servers", "admin", url.Values{"mode": {"range"}, "host": {"10.0.0.5"}, "port": {"27015"}, "port_end": {"27017"}, "hostname": {"Game"}, "rcon": {"secret"}})
	wantStatus(t, w, 303)
	if got := flashOf(t, w); got != "ok:3 servers added." {
		t.Errorf("flash = %q", got)
	}
	if len(e.st.servers) != 3 || e.st.created[0].PortEnd != 27017 {
		t.Fatalf("servers = %v created = %+v", e.st.servers, e.st.created)
	}

	w = e.do("POST", "/admin/servers", "admin", url.Values{"mode": {"hostname"}, "host": {"game.example.com"}, "port": {"27015"}, "hostname": {"H"}, "rcon": {"s"}})
	wantStatus(t, w, 303)
	if e.st.created[1].Mode != "hostname" || e.st.created[1].PortEnd != 0 {
		t.Errorf("created = %+v", e.st.created[1])
	}

	var id int64
	for k, s := range e.st.servers {
		if s.IP == "10.0.0.5:27016" {
			id = k
		}
	}
	w = e.do("GET", "/admin/servers", "viewer", nil)
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "10.0.0.5:27016") || strings.Contains(w.Body.String(), "/admin/servers/new") {
		t.Errorf("list wrong for viewer")
	}

	w = e.do("GET", "/admin/servers/"+strconv.FormatInt(id, 10)+"/edit", "admin", nil)
	wantStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "secret") {
		t.Error("rcon password rendered back")
	}

	edit := url.Values{"ip": {"10.0.0.9:27020"}, "hostname": {"Renamed"}, "rcon": {""}, "tv_ip": {"10.0.0.9:27025"}}
	w = e.do("POST", "/admin/servers/"+strconv.FormatInt(id, 10), "admin", edit)
	wantStatus(t, w, 303)
	got := e.st.servers[id]
	if got.IP != "10.0.0.9:27020" || got.RCON != "secret" || got.Hostname != "Renamed" {
		t.Errorf("after edit: %+v", got)
	}
	edit.Set("ip", "nonsense")
	wantStatus(t, e.do("POST", "/admin/servers/"+strconv.FormatInt(id, 10), "admin", edit), 422)

	s := e.st.servers[id]
	s.InUse = true
	e.st.servers[id] = s
	w = e.do("POST", "/admin/servers/"+strconv.FormatInt(id, 10)+"/delete", "admin", url.Values{})
	wantStatus(t, w, 303)
	if !strings.HasPrefix(flashOf(t, w), "err:") || len(e.st.servers) != 4 {
		t.Errorf("busy server was deleted or no error flash: %q", flashOf(t, w))
	}
	s.InUse = false
	e.st.servers[id] = s
	w = e.do("POST", "/admin/servers/"+strconv.FormatInt(id, 10)+"/delete", "admin", url.Values{})
	wantStatus(t, w, 303)
	if _, ok := e.st.servers[id]; ok {
		t.Error("server not deleted")
	}
	wantStatus(t, e.do("GET", "/admin/servers/999/edit", "admin", nil), 404)
	wantStatus(t, e.do("GET", "/admin/servers/abc/edit", "admin", nil), 404)
}

func TestSeasonCRUD(t *testing.T) {
	e := newEnv(t)
	good := url.Values{"name": {"Spring"}, "event": {"Cup"}, "start": {"2026-03-01"}, "end": {"2026-04-01"}, "active": {"1"}}

	bad := url.Values{"name": {""}, "event": {"Cup"}, "start": {"2026-03-01"}, "end": {"2026-02-01"}}
	w := e.do("POST", "/admin/seasons", "admin", bad)
	wantStatus(t, w, 422)
	body := w.Body.String()
	if !strings.Contains(body, "Required.") || !strings.Contains(body, "The end must not be before the start.") {
		t.Errorf("errors missing: %s", body)
	}

	wantStatus(t, e.do("POST", "/admin/seasons", "admin", good), 303)
	var id int64
	for k := range e.st.seasons {
		id = k
	}
	p := "/admin/seasons/" + strconv.FormatInt(id, 10)
	w = e.do("GET", p+"/edit", "admin", nil)
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `value="Spring"`) || !strings.Contains(w.Body.String(), " checked") {
		t.Errorf("edit form not filled")
	}

	good.Set("name", "Autumn")
	wantStatus(t, e.do("POST", p, "admin", good), 303)
	if e.st.seasons[id].Name != "Autumn" {
		t.Error("not updated")
	}
	wantStatus(t, e.do("POST", p+"/deactivate", "admin", url.Values{}), 303)
	if e.st.seasons[id].Active {
		t.Error("not deactivated")
	}
	wantStatus(t, e.do("POST", p+"/delete", "admin", url.Values{}), 303)
	if len(e.st.seasons) != 0 {
		t.Error("not deleted")
	}
	wantStatus(t, e.do("POST", p+"/delete", "admin", url.Values{}), 404)
}

func TestSeasonHTMLEscaped(t *testing.T) {
	e := newEnv(t)
	e.st.seasons[1] = store.Season{ID: 1, Name: `<script>alert(1)</script>`, Active: true}
	w := e.do("GET", "/admin/seasons", "admin", nil)
	if strings.Contains(w.Body.String(), "<script>alert") {
		t.Error("name not escaped")
	}
}

func TestAccessControl(t *testing.T) {
	e := newEnv(t)
	e.st.seasons[1] = store.Season{ID: 1, Name: "S"}

	w := e.do("GET", "/admin", "", nil)
	wantStatus(t, w, 303)
	if !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
	wantStatus(t, e.do("POST", "/admin/seasons", "", url.Values{}), 401)

	wantStatus(t, e.do("GET", "/admin", "viewer", nil), 200)
	wantStatus(t, e.do("GET", "/admin/teams", "viewer", nil), 200)
	for _, p := range []string{"/admin/seasons", "/admin/seasons/1", "/admin/seasons/1/delete", "/admin/seasons/1/deactivate", "/admin/servers", "/admin/users"} {
		wantStatus(t, e.do("POST", p, "viewer", url.Values{}), 403)
	}
	wantStatus(t, e.do("GET", "/admin/users", "viewer", nil), 403)
	wantStatus(t, e.do("GET", "/admin/users/new", "viewer", nil), 403)
	wantStatus(t, e.do("GET", "/admin/users", "", nil), 303)
	wantStatus(t, e.do("GET", "/admin/users", "admin", nil), 200)

	w = e.do("GET", "/admin", "admin", nil)
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "Dashboard") {
		t.Error("dashboard not rendered")
	}
}

func addUser(e *testEnv, id int64, name string, super bool) {
	e.st.users[id] = store.User{ID: id, Username: name, Email: name + "@example.com", IsActive: true, IsSuperAdmin: super}
}

func userForm(name string, super bool) url.Values {
	v := url.Values{"username": {name}, "email": {name + "@example.com"}, "is_active": {"1"}}
	if super {
		v.Set("is_super_admin", "1")
	}
	return v
}

func TestUsers(t *testing.T) {
	e := newEnv(t)
	addUser(e, 1, "root", true)
	addUser(e, 2, "other", true)
	addUser(e, 3, "plain", false)

	w := e.do("POST", "/admin/users", "admin", userForm("newbie", false))
	wantStatus(t, w, 422)
	if !strings.Contains(w.Body.String(), "Required.") {
		t.Error("password should be required on create")
	}
	f := userForm("newbie", false)
	f.Set("password", "pw1")
	f.Set("password_again", "pw2")
	w = e.do("POST", "/admin/users", "admin", f)
	wantStatus(t, w, 422)
	if !strings.Contains(w.Body.String(), "The two passwords must be the same.") {
		t.Error("mismatch not reported")
	}
	if strings.Contains(w.Body.String(), "pw1") {
		t.Error("password rendered back")
	}
	f.Set("password_again", "pw1")
	wantStatus(t, e.do("POST", "/admin/users", "admin", f), 303)

	dup := userForm("ROOT", false)
	dup.Set("email", "fresh@example.com")
	dup.Set("password", "x")
	dup.Set("password_again", "x")
	w = e.do("POST", "/admin/users", "admin", dup)
	wantStatus(t, w, 422)
	if !strings.Contains(w.Body.String(), "This username is already in use.") {
		t.Errorf("duplicate username not reported: %s", w.Body.String())
	}
	dup = userForm("fresh", false)
	dup.Set("email", "other@example.com")
	dup.Set("password", "x")
	dup.Set("password_again", "x")
	w = e.do("POST", "/admin/users", "admin", dup)
	wantStatus(t, w, 422)
	if !strings.Contains(w.Body.String(), "This email address is already in use.") {
		t.Errorf("duplicate email not reported")
	}

	w = e.doAs("GET", "/admin/users/3/edit", "admin", "root", nil)
	wantStatus(t, w, 200)

	w = e.doAs("POST", "/admin/users/1/delete", "admin", "root", url.Values{})
	wantStatus(t, w, 303)
	if _, ok := e.st.users[1]; !ok || !strings.HasPrefix(flashOf(t, w), "err:") {
		t.Errorf("self delete: %q", flashOf(t, w))
	}

	w = e.doAs("POST", "/admin/users/1", "admin", "root", userForm("root", false))
	wantStatus(t, w, 422)
	if !e.st.users[1].IsSuperAdmin {
		t.Error("self demotion went through")
	}

	w = e.doAs("POST", "/admin/users/2", "admin", "root", userForm("other", false))
	wantStatus(t, w, 303)
	if e.st.users[2].IsSuperAdmin {
		t.Error("demoting another super admin should work while root remains")
	}

	// root is now the only super admin. Another admin session may not remove it.
	w = e.doAs("POST", "/admin/users/1/delete", "admin", "someone-else", url.Values{})
	wantStatus(t, w, 303)
	if _, ok := e.st.users[1]; !ok || !strings.HasPrefix(flashOf(t, w), "err:") {
		t.Errorf("last super admin deleted: %q", flashOf(t, w))
	}
	w = e.doAs("POST", "/admin/users/1", "admin", "someone-else", userForm("root", false))
	wantStatus(t, w, 422)

	wantStatus(t, e.doAs("POST", "/admin/users/3/delete", "admin", "root", url.Values{}), 303)
	if _, ok := e.st.users[3]; ok {
		t.Error("plain user not deleted")
	}
}
