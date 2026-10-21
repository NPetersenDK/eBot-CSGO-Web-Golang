package auth

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func issued(t *testing.T, m *Manager, s Session) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.Issue(rec, s); err != nil {
		t.Fatal(err)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies = %d", len(cs))
	}
	return cs[0]
}

func reqWith(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestSessionRoundTrip(t *testing.T) {
	m := NewManager(testKey, true)
	c := issued(t, m, Session{Subject: "bob", Name: "Bob", Role: RoleAdmin, Source: SourceLocal})
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Name != "ebot_session" {
		t.Errorf("cookie attributes wrong: %+v", c)
	}
	s, ok := m.Read(reqWith(c))
	if !ok || s.Subject != "bob" || s.Name != "Bob" || s.Role != RoleAdmin || s.Source != SourceLocal {
		t.Fatalf("read = %+v %v", s, ok)
	}
	if d := time.Until(s.Expires); d < 11*time.Hour || d > 12*time.Hour+time.Minute {
		t.Errorf("expires in %v", d)
	}
}

func TestSessionTamper(t *testing.T) {
	m := NewManager(testKey, false)
	c := issued(t, m, Session{Subject: "bob", Role: RoleViewer, Source: SourceOIDC})
	payload, sig, _ := strings.Cut(c.Value, ".")

	cases := map[string]string{
		"flipped last char": c.Value[:len(c.Value)-1] + "A",
		"no dot":            payload + sig,
		"empty sig":         payload + ".",
		"swapped":           sig + "." + payload,
		"garbage":           "!!.!!",
	}
	for name, v := range cases {
		if v == c.Value {
			continue
		}
		if _, ok := m.Read(reqWith(&http.Cookie{Name: CookieName, Value: v})); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	other := NewManager([]byte("ffffffffffffffffffffffffffffffff"), false)
	if _, ok := other.Read(reqWith(c)); ok {
		t.Error("accepted with another key")
	}
	if _, ok := m.Read(reqWith(nil)); ok {
		t.Error("accepted without cookie")
	}
}

func TestSessionExpiredAndInvalidFields(t *testing.T) {
	m := NewManager(testKey, false)
	sign := func(c string) *http.Cookie {
		return &http.Cookie{Name: CookieName, Value: m.sign([]byte(c))}
	}
	past := time.Now().Add(-time.Minute).Unix()
	future := time.Now().Add(time.Hour).Unix()
	if _, ok := m.Read(reqWith(sign(`{"sub":"a","role":"admin","src":"local","exp":` + itoa(past) + `}`))); ok {
		t.Error("expired accepted")
	}
	if _, ok := m.Read(reqWith(sign(`{"sub":"a","role":"admin","src":"local","exp":` + itoa(future) + `}`))); !ok {
		t.Error("valid rejected")
	}
	if _, ok := m.Read(reqWith(sign(`{"sub":"a","role":"root","src":"local","exp":` + itoa(future) + `}`))); ok {
		t.Error("unknown role accepted")
	}
	if _, ok := m.Read(reqWith(sign(`{"sub":"a","role":"admin","src":"x","exp":` + itoa(future) + `}`))); ok {
		t.Error("unknown source accepted")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

type fakeRoles struct {
	admin bool
	err   error
}

func (f fakeRoles) IsAdmin(context.Context, string) (bool, error) { return f.admin, f.err }

func runMiddleware(t *testing.T, m *Manager, roles RoleChecker, c *http.Cookie) (Session, bool, *httptest.ResponseRecorder) {
	t.Helper()
	var got Session
	var ok bool
	h := m.Middleware(roles)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = FromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, reqWith(c))
	return got, ok, rec
}

func TestMiddleware(t *testing.T) {
	m := NewManager(testKey, false)
	local := issued(t, m, Session{Subject: "bob", Role: RoleAdmin, Source: SourceLocal})
	sso := issued(t, m, Session{Subject: "sso", Role: RoleViewer, Source: SourceOIDC})

	if _, ok, _ := runMiddleware(t, m, fakeRoles{admin: true}, local); !ok {
		t.Error("admin local session dropped")
	}
	if _, ok, rec := runMiddleware(t, m, fakeRoles{admin: false}, local); ok {
		t.Error("demoted user kept session")
	} else if cs := rec.Result().Cookies(); len(cs) != 1 || cs[0].MaxAge != -1 {
		t.Error("cookie not cleared")
	}
	if _, ok, _ := runMiddleware(t, m, fakeRoles{err: ErrInvalidCredentials}, local); ok {
		t.Error("not-found user kept session")
	}
	if _, ok, _ := runMiddleware(t, m, fakeRoles{err: errors.New("db down")}, local); !ok {
		t.Error("transient error dropped session")
	}
	if _, ok, _ := runMiddleware(t, m, fakeRoles{admin: false}, sso); !ok {
		t.Error("oidc session was re-checked")
	}
	if _, ok, _ := runMiddleware(t, m, nil, local); !ok {
		t.Error("nil roles dropped session")
	}
	if _, ok, _ := runMiddleware(t, m, nil, nil); ok {
		t.Error("anonymous got a session")
	}
}

func TestRoleMap(t *testing.T) {
	m := newRoleMap("roles", "ebot-view=viewer, ebot-admin=admin,bogus=root,junk", "deny")
	if len(m.Map) != 2 {
		t.Fatalf("map = %+v", m.Map)
	}
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"admin wins", []string{"ebot-view", "ebot-admin"}, RoleAdmin},
		{"viewer", []string{"ebot-view"}, RoleViewer},
		{"no match denies", []string{"other"}, roleDeny},
		{"empty denies", nil, roleDeny},
		{"unknown role entry skipped", []string{"bogus"}, roleDeny},
	}
	for _, c := range cases {
		if got := m.resolve(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	if got := newRoleMap("roles", "a=admin", "viewer").resolve(nil); got != RoleViewer {
		t.Errorf("default viewer: %q", got)
	}
	if got := newRoleMap("roles", "", "nonsense").resolve(nil); got != roleDeny {
		t.Errorf("invalid default: %q", got)
	}
	first := newRoleMap("roles", "a=viewer,b=viewer", "deny")
	if got := first.resolve([]string{"b", "a"}); got != RoleViewer {
		t.Errorf("first match: %q", got)
	}
}

func TestResolveClaims(t *testing.T) {
	o := &oidcClient{roles: newRoleMap("groups", "g-admin=admin,g-view=viewer", "deny")}
	res, err := o.resolve("sub1", map[string]any{
		"groups":             []any{"g-view", "g-admin", 7},
		"preferred_username": "bob",
		"email":              "b@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Session
	if s.Role != RoleAdmin || s.Subject != "sub1" || s.Name != "bob" || s.Source != SourceOIDC {
		t.Errorf("session = %+v", s)
	}
	res, err = o.resolve("sub2", map[string]any{"groups": "g-view", "name": "Alice", "preferred_username": "al"})
	if err != nil || res.Session.Role != RoleViewer || res.Session.Name != "Alice" {
		t.Errorf("scalar claim: %+v %v", res, err)
	}
	_, err = o.resolve("sub3", map[string]any{"groups": []any{"x", "y"}, "email": "c@example.com"})
	var d *deniedError
	if !errors.As(err, &d) {
		t.Fatalf("err = %v", err)
	}
	if d.page.Subject != "sub3" || d.page.Email != "c@example.com" || d.page.Claim != "groups" || len(d.page.Values) != 2 {
		t.Errorf("denied page = %+v", d.page)
	}
}

func TestHintFor(t *testing.T) {
	ps := []LoginProvider{{Name: "A", Hint: "a"}, {Name: "Default"}}
	if hintFor(ps, "a") != "a" || hintFor(ps, "evil") != "" || hintFor(ps, "") != "" || hintFor(nil, "a") != "" {
		t.Error("hint filtering wrong")
	}
	got, err := parseLoginProviders(`[{"name":"Discord","hint":"discord","icon":"d"}]`)
	if err != nil || len(got) != 1 || got[0].Hint != "discord" {
		t.Errorf("parse = %+v %v", got, err)
	}
	if _, err := parseLoginProviders(`{bad`); err == nil {
		t.Error("bad json accepted")
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLoginLimiter(3, 100, 5*time.Minute, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !l.allow("k", "1.1.1.1") {
			t.Fatalf("blocked after %d", i)
		}
	}
	if l.allow("k", "1.1.1.1") {
		t.Error("not blocked at limit")
	}
	if !l.allow("other", "1.1.1.1") {
		t.Error("other user blocked")
	}
	if !l.allow("k", "2.2.2.2") {
		t.Error("same user from another address blocked")
	}
	now = now.Add(5*time.Minute + time.Second)
	if !l.allow("k", "1.1.1.1") {
		t.Error("still blocked after window")
	}
	l.allow("k", "1.1.1.1")
	l.success("k", "1.1.1.1")
	for i := 0; i < 3; i++ {
		if !l.allow("k", "1.1.1.1") {
			t.Fatalf("success did not clear, attempt %d", i)
		}
	}
}

func TestRateLimiterPerAddress(t *testing.T) {
	l := newLoginLimiter(10, 5, time.Minute, nil)
	for i := 0; i < 5; i++ {
		if !l.allow("user"+strconv.Itoa(i), "1.1.1.1") {
			t.Fatalf("blocked at %d", i)
		}
	}
	if l.allow("fresh", "1.1.1.1") {
		t.Error("address limit not applied across usernames")
	}
	if !l.allow("fresh", "2.2.2.2") {
		t.Error("other address blocked")
	}
}

func TestRateLimiterRefund(t *testing.T) {
	l := newLoginLimiter(2, 2, time.Minute, nil)
	for i := 0; i < 5; i++ {
		if !l.allow("u", "1.1.1.1") {
			t.Fatalf("refunded attempt %d counted", i)
		}
		l.refund("u", "1.1.1.1")
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	l := newLoginLimiter(10, 1000, time.Minute, nil)
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("same", "1.1.1.1") {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 10 {
		t.Errorf("%d concurrent attempts passed, want 10", passed.Load())
	}

	l = newLoginLimiter(1000, 20, time.Minute, nil)
	passed.Store(0)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("u"+strconv.Itoa(i), "1.1.1.1") {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 20 {
		t.Errorf("%d attempts passed the address limit, want 20", passed.Load())
	}
}

func TestRateLimiterKeyCap(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLoginLimiter(10, 10, time.Minute, func() time.Time { return now })
	attempt := func(i int) {
		now = now.Add(time.Millisecond)
		l.allow("u"+strconv.Itoa(i), "10.0."+strconv.Itoa(i/250)+"."+strconv.Itoa(i%250))
		if len(l.attempts) > maxTrackedKeys {
			t.Fatalf("%d keys tracked", len(l.attempts))
		}
	}
	n := 0
	for ; len(l.attempts) < maxTrackedKeys; n++ {
		attempt(n)
	}
	attempt(n)
	if _, ok := l.attempts[userKeyPrefix+"u0|10.0.0.0"]; ok {
		t.Error("oldest key was not evicted")
	}
	if _, ok := l.attempts[userKeyPrefix+"u"+strconv.Itoa(n)+"|10.0."+strconv.Itoa(n/250)+"."+strconv.Itoa(n%250)]; !ok {
		t.Error("newest key was evicted")
	}

	for len(l.attempts) < maxTrackedKeys {
		n++
		attempt(n)
	}
	now = now.Add(2 * time.Minute)
	l.allow("late", "2.2.2.2")
	if len(l.attempts) != 2 {
		t.Errorf("expired keys kept: %d", len(l.attempts))
	}
}

func TestLocalNext(t *testing.T) {
	good := map[string]string{
		"/":                 "/",
		"/matches?status=a": "/matches?status=a",
		"/admin/servers":    "/admin/servers",
	}
	for in, want := range good {
		if got := localNext(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
	for _, in := range []string{"", "//evil.com", "/\\evil.com", "https://evil.com", "javascript:alert(1)", "evil.com", "/\t/evil.com", "/a\\b", "/a\r\nb", "\\\\evil.com"} {
		if got := localNext(in); got != "/" {
			t.Errorf("%q -> %q", in, got)
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	if got := clientIP(r, true); got != "10.0.0.1" {
		t.Errorf("remote: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := clientIP(r, true); got != "2.2.2.2" {
		t.Errorf("xff: %q", got)
	}
	r.Header.Add("X-Forwarded-For", "6.6.6.6, 3.3.3.3")
	if got := clientIP(r, true); got != "3.3.3.3" {
		t.Errorf("xff with two lines: %q", got)
	}
	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted: %q", got)
	}
}

type fakeLocal struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLocal) VerifyLocal(_ context.Context, u, p string) (Session, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if u == "admin" && p == "secret" {
		return Session{Subject: "admin", Name: "admin", Role: RoleAdmin, Source: SourceLocal}, nil
	}
	if u == "boom" {
		return Session{}, errors.New("db down")
	}
	return Session{}, ErrInvalidCredentials
}

type rendered struct {
	status int
	page   string
	data   any
}

func newTestHandler(t *testing.T, cfg *config.Config) (*Handler, *http.ServeMux, *fakeLocal, *rendered) {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{Title: "eBot"}
	}
	fl := &fakeLocal{}
	rd := &rendered{}
	h, err := NewHandler(context.Background(), cfg, NewManager(testKey, false), fl, func(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
		rd.status, rd.page, rd.data = status, page, data
		w.WriteHeader(status)
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	return h, mux, fl, rd
}

func postLogin(mux *http.ServeMux, user, pass, next string) *httptest.ResponseRecorder {
	form := url.Values{"username": {user}, "password": {pass}, "next": {next}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "9.9.9.9:1"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestLoginHandler(t *testing.T) {
	_, mux, _, rd := newTestHandler(t, nil)

	rec := postLogin(mux, "admin", "secret", "/admin/teams")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/teams" {
		t.Fatalf("success: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != CookieName {
		t.Fatalf("cookies = %+v", cs)
	}

	rec = postLogin(mux, "admin", "secret", "//evil.com")
	if rec.Header().Get("Location") != "/" {
		t.Errorf("open redirect: %q", rec.Header().Get("Location"))
	}

	rec = postLogin(mux, "admin", "wrong", "/x")
	if rec.Code != http.StatusUnauthorized || rd.page != "login" {
		t.Errorf("wrong password: %d %q", rec.Code, rd.page)
	}
	if lp := rd.data.(LoginPage); lp.Error == "" || lp.Next != "/x" || lp.Title() == "" {
		t.Errorf("page = %+v", lp)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("cookie set on failure")
	}

	rec = postLogin(mux, "boom", "x", "/")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("backend error: %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, mux, fl, _ := newTestHandler(t, nil)
	for i := 0; i < 10; i++ {
		if rec := postLogin(mux, "Admin", "bad", "/"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	before := fl.calls
	if rec := postLogin(mux, "admin", "secret", "/"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("blocked login: %d", rec.Code)
	}
	if fl.calls != before {
		t.Error("verify called while blocked")
	}
	if rec := postLogin(mux, "other", "bad", "/"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other user blocked: %d", rec.Code)
	}
}

func TestLoginPageAndLogout(t *testing.T) {
	h, mux, _, rd := newTestHandler(t, &config.Config{Title: "eBot"})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/login?next=//evil.com", nil))
	if rec.Code != 200 || rd.page != "login" || rd.data.(LoginPage).Next != "/" || !rd.data.(LoginPage).LocalLogin {
		t.Errorf("login page: %d %+v", rec.Code, rd)
	}

	c := issued(t, h.m, Session{Subject: "a", Role: RoleAdmin, Source: SourceLocal})
	r := httptest.NewRequest("GET", "/login?next=/matches", nil)
	r.AddCookie(c)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/matches" {
		t.Errorf("logged in redirect: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/logout", nil))
	cs := rec.Result().Cookies()
	if rec.Header().Get("Location") != "/" || len(cs) == 0 || cs[0].MaxAge != -1 {
		t.Errorf("logout: %q %+v", rec.Header().Get("Location"), cs)
	}
	paths := map[string]bool{}
	for _, c := range cs {
		if c.Name == idTokenCookie {
			paths[c.Path] = c.MaxAge == -1
		}
	}
	if !paths["/logout"] || !paths["/"] {
		t.Errorf("id token cookie not cleared on both paths: %v", paths)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/logout", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /logout: %d", rec.Code)
	}
}

func TestIDTokenCookiePath(t *testing.T) {
	h, _, _, _ := newTestHandler(t, nil)
	rec := httptest.NewRecorder()
	h.writeIDToken(rec, "tok")
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].Path != "/logout" || !cs[0].HttpOnly {
		t.Errorf("cookie = %+v", cs)
	}
}

func TestLoginHardening(t *testing.T) {
	_, mux, fl, _ := newTestHandler(t, nil)

	long := strings.Repeat("a", 129)
	if rec := postLogin(mux, long, "x", "/"); rec.Code != http.StatusUnauthorized || fl.calls != 0 {
		t.Errorf("long username: %d, verify calls %d", rec.Code, fl.calls)
	}

	form := url.Values{"username": {"admin"}, "password": {strings.Repeat("p", 9<<10)}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge || fl.calls != 0 {
		t.Errorf("big body: %d, verify calls %d", rec.Code, fl.calls)
	}
}

func TestLoginAddressLimitIsOffWithoutATrustedProxy(t *testing.T) {
	_, mux, _, _ := newTestHandler(t, nil)
	for i := 0; i < 60; i++ {
		if rec := postLogin(mux, "user"+strconv.Itoa(i), "bad", "/"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d from a shared address: %d", i, rec.Code)
		}
	}
	if rec := postLogin(mux, "admin", "secret", "/"); rec.Code == http.StatusTooManyRequests {
		t.Error("one visitor must not be able to lock everybody out")
	}
}

func TestLoginAddressLimit(t *testing.T) {
	_, mux, fl, _ := newTestHandler(t, &config.Config{Title: "eBot", TrustProxy: true})
	for i := 0; i < 30; i++ {
		if rec := postLogin(mux, "user"+strconv.Itoa(i), "bad", "/"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	before := fl.calls
	if rec := postLogin(mux, "admin", "secret", "/"); rec.Code != http.StatusTooManyRequests || fl.calls != before {
		t.Errorf("address not limited: %d", rec.Code)
	}
}

func TestLogName(t *testing.T) {
	if got := logName(strings.Repeat("é", 100)); len([]rune(got)) != 64 {
		t.Errorf("len = %d", len([]rune(got)))
	}
	if logName("admin") != "admin" {
		t.Error("short name changed")
	}
}

func TestLoginConcurrentWrongPasswords(t *testing.T) {
	fl := &fakeLocal{}
	h, err := NewHandler(context.Background(), &config.Config{Title: "eBot"}, NewManager(testKey, false), fl, func(w http.ResponseWriter, _ *http.Request, status int, _ string, _ any) {
		w.WriteHeader(status)
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			postLogin(mux, "admin", "bad", "/")
		}()
	}
	wg.Wait()
	if fl.calls > 10 {
		t.Errorf("%d passwords were checked, limit is 10", fl.calls)
	}
}

func TestLocalLoginDisabled(t *testing.T) {
	_, mux, fl, _ := newTestHandler(t, &config.Config{LocalLoginDisabled: true})
	if rec := postLogin(mux, "admin", "secret", "/"); rec.Code != http.StatusForbidden || fl.calls != 0 {
		t.Errorf("code %d calls %d", rec.Code, fl.calls)
	}
}

func TestOIDCRoutesDisabled(t *testing.T) {
	_, mux, _, _ := newTestHandler(t, nil)
	for _, p := range []string{"/auth/oidc/login", "/auth/oidc/callback"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestLoginPageProviders(t *testing.T) {
	h, _, _, _ := newTestHandler(t, &config.Config{Title: "x", LoginProviders: `[{"name":"Discord","hint":"discord"}]`})
	h.oidc = &oidcClient{}
	p := h.page(httptest.NewRequest("GET", "/login", nil), "/matches?a=b", "")
	if len(p.Providers) != 1 || p.Providers[0].URL != "/auth/oidc/login?idp=discord&next=%2Fmatches%3Fa%3Db" {
		t.Errorf("providers = %+v", p.Providers)
	}
	h.providers = nil
	p = h.page(httptest.NewRequest("GET", "/login", nil), "/", "")
	if len(p.Providers) != 1 || p.Providers[0].Name == "" {
		t.Errorf("default button = %+v", p.Providers)
	}
}

func TestTemplates(t *testing.T) {
	fsys := os.DirFS("../../web")
	funcs := template.FuncMap{"join": strings.Join, "hasPrefix": strings.HasPrefix}
	render := func(page string, data any) string {
		tpl, err := template.New("layout").Funcs(funcs).ParseFS(fsys, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		view := map[string]any{"Title": "t", "Brand": "eBot", "Path": "/", "User": nil, "IsAdmin": false, "Flash": nil, "Cfg": &config.Config{}, "Data": data}
		if err := tpl.ExecuteTemplate(&buf, "layout", view); err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		return buf.String()
	}
	out := render("login", LoginPage{Next: "/x", Error: "bad", LocalLogin: true, Providers: []LoginProviderLink{{Name: "SSO", URL: "/auth/oidc/login?next=%2F"}}})
	for _, want := range []string{`name="next" value="/x"`, "bad", "SSO", `href="/auth/oidc/login?next=%2F"`} {
		if !strings.Contains(out, want) {
			t.Errorf("login missing %q", want)
		}
	}
	out = render("denied", DeniedPage{Subject: "s1", Name: "<b>x</b>", Claim: "roles", Values: []string{"a", "b"}})
	if !strings.Contains(out, "s1") || !strings.Contains(out, "a, b") || strings.Contains(out, "<b>x</b>") {
		t.Errorf("denied output wrong: %s", out)
	}
}

func TestClientIPIgnoresForwardedForUnlessTrusted(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted proxy: %q", got)
	}
}

func TestIPLimitOnlyWithATrustedProxy(t *testing.T) {
	if ipLimit(false) != 0 || ipLimit(true) != 30 {
		t.Fatalf("ipLimit: %d %d", ipLimit(false), ipLimit(true))
	}
	// Behind a proxy that is not trusted every visitor shares one address. One
	// person failing many times under different names must not block the rest.
	l := newLoginLimiter(10, 0, time.Minute, nil)
	for i := 0; i < 200; i++ {
		if !l.allow("user"+strconv.Itoa(i), "10.0.0.1") {
			t.Fatalf("attempt %d blocked with the address limit off", i)
		}
	}
	if len(l.attempts) != 200 {
		t.Errorf("tracked %d keys, want 200 (no address keys)", len(l.attempts))
	}
	for i := 0; i < 10; i++ {
		l.allow("victim", "10.0.0.1")
	}
	if l.allow("victim", "10.0.0.1") {
		t.Error("the username limit must still hold")
	}
}

func TestFloodingTheTableDoesNotFreeABlockedKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLoginLimiter(3, 0, 5*time.Minute, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		l.allow("target", "1.2.3.4")
	}
	if l.allow("target", "1.2.3.4") {
		t.Fatal("target should be blocked")
	}
	// Fill the table with other names, then keep adding so eviction runs.
	for i := 0; i < maxTrackedKeys+2500; i++ {
		now = now.Add(time.Millisecond)
		l.allow("n"+strconv.Itoa(i), "9.9.9."+strconv.Itoa(i%250))
	}
	if l.allow("target", "1.2.3.4") {
		t.Error("an attacker's flood reset the counter of a blocked key")
	}
	if len(l.attempts) > maxTrackedKeys {
		t.Errorf("table grew to %d", len(l.attempts))
	}
}
