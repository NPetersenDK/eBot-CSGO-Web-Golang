package web

import (
	"net/http/httptest"
	"strings"
	"testing"

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
