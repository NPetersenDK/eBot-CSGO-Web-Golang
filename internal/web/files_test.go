package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

func TestDemoDownload(t *testing.T) {
	e := newEnv(t)
	dir := e.app.Cfg.DemoPath
	if err := os.WriteFile(filepath.Join(dir, "match1.dem.zip"), []byte("zipdata"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.st.maps[5] = store.Map{ID: 5, MatchID: 1, TVRecordFile: "match1"}

	w := e.do("GET", "/matches/1/demo/5", "", nil)
	wantStatus(t, w, 200)
	if w.Body.String() != "zipdata" {
		t.Errorf("body = %q", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename=match1.dem.zip` {
		t.Errorf("content-disposition = %q", cd)
	}

	wantStatus(t, e.do("GET", "/matches/2/demo/5", "", nil), 404)
	wantStatus(t, e.do("GET", "/matches/1/demo/6", "", nil), 404)
	wantStatus(t, e.do("GET", "/matches/1/demo/abc", "", nil), 404)

	e.app.Cfg.DemoDownload = false
	wantStatus(t, e.do("GET", "/matches/1/demo/5", "", nil), 404)
}

func TestDemoTraversal(t *testing.T) {
	e := newEnv(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.dem.zip"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.app.Cfg.DemoPath), "up.dem.zip"), []byte("up"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.dem.zip"), filepath.Join(e.app.Cfg.DemoPath, "link.dem.zip")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.app.Cfg.DemoPath, "dir.dem.zip"), 0o700); err != nil {
		t.Fatal(err)
	}

	names := []string{"../../etc/passwd", "/etc/passwd", "..", ".", "", "../up", "a/b", `..\up`, "link", "dir", "x\x00y", "/"}
	for i, name := range names {
		id := int64(10 + i)
		e.st.maps[id] = store.Map{ID: id, MatchID: 1, TVRecordFile: name}
		w := e.do("GET", "/matches/1/demo/"+itoa(id), "", nil)
		if w.Code != 404 {
			t.Errorf("name %q: status %d, body %q", name, w.Code, w.Body.String())
		}
	}
}

func itoa(n int64) string { return int64Text(n) }

func TestSafeFileName(t *testing.T) {
	for name, want := range map[string]bool{
		"match1": true, "a.b": true, "": false, ".": false, "..": false, "a/b": false, "/a": false, "a/": false, "./a": false,
	} {
		if got := safeFileName(name); got != want {
			t.Errorf("safeFileName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestMatchLog(t *testing.T) {
	e := newEnv(t)
	dir := e.app.Cfg.LogMatchAdminPath
	if err := os.WriteFile(filepath.Join(dir, "match-7.html"), []byte("<b>log</b><script>alert(1)</script>"), 0o600); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, e.do("GET", "/admin/matches/7/log", "", nil), 303)
	wantStatus(t, e.do("GET", "/admin/matches/7/log/raw", "", nil), 303)

	w := e.do("GET", "/admin/matches/7/log", "viewer", nil)
	wantStatus(t, w, 200)
	body := w.Body.String()
	for _, want := range []string{`<iframe class="log" sandbox`, `src="/admin/matches/7/log/raw"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	w = e.do("GET", "/admin/matches/7/log/raw", "viewer", nil)
	wantStatus(t, w, 200)
	h := w.Header()
	if h.Get("Content-Security-Policy") != "sandbox; default-src 'none'; style-src 'unsafe-inline'" {
		t.Errorf("csp = %q", h.Get("Content-Security-Policy"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("headers = %v", h)
	}
	if !strings.Contains(w.Body.String(), "<b>log</b>") {
		t.Error("log content missing")
	}

	wantStatus(t, e.do("GET", "/admin/matches/8/log", "viewer", nil), 404)
	wantStatus(t, e.do("GET", "/admin/matches/8/log/raw", "viewer", nil), 404)
	wantStatus(t, e.do("GET", "/admin/matches/0/log/raw", "viewer", nil), 404)
	wantStatus(t, e.do("GET", "/admin/matches/-7/log/raw", "viewer", nil), 404)
	wantStatus(t, e.do("GET", "/admin/matches/07x/log/raw", "viewer", nil), 404)
}
