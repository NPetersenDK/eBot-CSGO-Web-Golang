package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
)

func navOf(body string) string {
	a := strings.Index(body, `<nav class="nav">`)
	b := strings.Index(body, "</nav>")
	if a < 0 || b < a {
		return ""
	}
	return body[a:b]
}

func TestNavigation(t *testing.T) {
	e := newMatchTestEnv(t)

	admin := e.do("GET", "/matches", "admin", nil).Body.String()
	nav := navOf(admin)
	for _, want := range []string{
		"Matches", "Create match", "Current matches", "Archived matches",
		"Management", "Servers", "Teams", "Seasons", "Advertisement", "Statistics",
		`href="/admin/matches/new"`, `href="/matches"`, `href="/matches?status=archived"`,
		`href="/admin/servers"`, `href="/admin/teams"`, `href="/admin/seasons"`, `href="/admin/ads"`,
	} {
		if !strings.Contains(nav, want) {
			t.Errorf("admin nav missing %q", want)
		}
	}
	for _, gone := range []string{"New match", ">Archived<", "Advertising", "/admin/users"} {
		if strings.Contains(nav, gone) {
			t.Errorf("admin nav still has %q", gone)
		}
	}
	// Users moved into the user menu, next to Log out.
	menu := admin[strings.Index(admin, "user-profile-button"):]
	if !strings.Contains(menu, `href="/admin/users"`) || strings.Index(menu, `href="/admin/users"`) > strings.Index(menu, "Log out") {
		t.Error("Users must sit in the user menu above Log out")
	}
	if !strings.Contains(nav, `summary class="nav-link active"`) {
		t.Error("the Matches menu must be marked active on /matches")
	}

	viewer := e.do("GET", "/matches", "viewer", nil).Body.String()
	vnav := navOf(viewer)
	if strings.Contains(vnav, "Create match") {
		t.Error("a viewer must not be offered Create match")
	}
	if !strings.Contains(vnav, "Management") || !strings.Contains(vnav, "Servers") {
		t.Error("a viewer can read the management lists")
	}
	if strings.Contains(viewer, `href="/admin/users"`) {
		t.Error("a viewer must not see Users")
	}

	anon := e.do("GET", "/matches", "", nil).Body.String()
	anav := navOf(anon)
	if !strings.Contains(anav, "Current matches") || !strings.Contains(anav, "Archived matches") || !strings.Contains(anav, "Statistics") {
		t.Error("anonymous visitors get the public links")
	}
	for _, gone := range []string{"Management", "Create match", "Servers"} {
		if strings.Contains(anav, gone) {
			t.Errorf("anonymous nav has %q", gone)
		}
	}
}

func TestNavigationMarksManagementActive(t *testing.T) {
	e := newMatchTestEnv(t)
	for _, path := range []string{"/admin/servers", "/admin/teams/3/edit", "/admin/seasons", "/admin/ads/new"} {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(auth.WithSession(req.Context(), auth.Session{Subject: "a", Name: "a", Role: auth.RoleAdmin}))
		rec := httptest.NewRecorder()
		e.app.NotFound(rec, req)
		nav := navOf(rec.Body.String())
		i := strings.Index(nav, "Management")
		if i < 0 || !strings.Contains(nav[:i], `summary class="nav-link active"`) {
			t.Errorf("Management must be active on %s", path)
		}
		if strings.Contains(nav[:strings.Index(nav, "Statistics")], `Matches <svg`) && strings.Contains(nav[:strings.Index(nav, "Matches <svg")], "active") {
			t.Errorf("Matches must not be active on %s", path)
		}
	}
}

func TestConfirmBoxesDoNotSitInTheRow(t *testing.T) {
	e := newMatchTestEnv(t)
	for _, path := range []string{"/matches", "/matches/6"} {
		body := e.do("GET", path, "admin", nil).Body.String()
		if !strings.Contains(body, `popover class="confirm-box"`) {
			t.Errorf("%s has no confirm box", path)
		}
		if strings.Contains(body, "Confirm: ") {
			t.Errorf("%s still has the inline confirm button", path)
		}
		// Every button points at a box that exists, and every box has a Cancel.
		for _, id := range between(body, `popovertarget="`, `"`) {
			if !strings.Contains(body, `id="`+id+`" popover`) {
				t.Errorf("%s: popovertarget %q has no box", path, id)
			}
		}
		if n, c := strings.Count(body, `class="confirm-box"`), strings.Count(body, `popovertargetaction="hide"`); n != c {
			t.Errorf("%s: %d boxes but %d cancel buttons", path, n, c)
		}
	}
}

func between(s, open, closeStr string) []string {
	var out []string
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return out
		}
		s = s[i+len(open):]
		j := strings.Index(s, closeStr)
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j:]
	}
}

func TestFlashIsSigned(t *testing.T) {
	e := newMatchTestEnv(t)
	e.app.Cfg.SessionKey = []byte("0123456789abcdef0123456789abcdef")

	rec := httptest.NewRecorder()
	e.app.SetFlash(rec, "ok", "Saved.")
	cookie := rec.Result().Cookies()[0]
	if !strings.Contains(cookie.Value, ".") {
		t.Fatalf("flash cookie is not signed: %q", cookie.Value)
	}

	show := func(value string, header string) string {
		req := httptest.NewRequest("GET", "/matches", nil)
		req.AddCookie(&http.Cookie{Name: flashCookie, Value: value})
		if header != "" {
			req.Header.Set("X-Live-Refresh", header)
		}
		rec := httptest.NewRecorder()
		e.app.NotFound(rec, req)
		return rec.Body.String() + "|" + rec.Header().Get("Set-Cookie")
	}
	if got := show(cookie.Value, ""); !strings.Contains(got, "Saved.") {
		t.Error("a signed flash must show")
	}

	forged := base64.RawURLEncoding.EncodeToString([]byte("err:Your account is locked, call 555-1234")) + "." + strings.SplitN(cookie.Value, ".", 2)[1]
	if got := show(forged, ""); strings.Contains(got, "555-1234") {
		t.Error("a flash with a signature for other text must be ignored")
	}
	unsigned := base64.RawURLEncoding.EncodeToString([]byte("err:Planted"))
	if got := show(unsigned, ""); strings.Contains(got, "Planted") {
		t.Error("an unsigned flash must be ignored")
	}

	// A background refresh must leave the flash for the page that is loading.
	got := show(cookie.Value, "1")
	if strings.Contains(got, "Saved.") || strings.Contains(got, flashCookie) {
		t.Errorf("a live refresh must not show or clear the flash: %s", got)
	}
}

func TestStartDateUsesTheConfiguredZone(t *testing.T) {
	e := newMatchTestEnv(t)
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatal(err)
	}
	e.app.Cfg.Location = loc

	v := validMatchForm()
	v.Set("startdate", "21.10.2026 15:00")
	in, errs := parseMatchWith(t, e, v, nil)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if got := in.StartDate.UTC().Format("2006-01-02 15:04"); got != "2026-10-21 13:00" {
		t.Errorf("stored as %s UTC, want 13:00 (15:00 in Copenhagen, UTC+2)", got)
	}

	// Showing it again gives what the admin typed, so saving does not move it.
	m := sampleMatch()
	utc := in.StartDate.UTC()
	m.StartDate = &utc
	if got := formFromMatch(m, nil, loc).StartDate; got != "21.10.2026 15:00" {
		t.Errorf("form shows %q", got)
	}
	if got := fmtTime("2006-01-02 15:04", loc)(m.StartDate); got != "2026-10-21 15:04"[:11]+"15:00" {
		t.Errorf("page shows %q", got)
	}
}
