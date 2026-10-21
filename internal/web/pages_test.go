package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

func TestMatchPageAdminDetails(t *testing.T) {
	e := newMatchTestEnv(t)
	body := e.do("GET", "/matches/5", "admin", nil).Body.String()

	if !strings.Contains(body, `<details data-keep="rounds-10">`) {
		t.Error("round summaries need a data-keep key")
	}
	if !strings.Contains(body, "Reload to start of round 4") {
		t.Error("the current map offers a reload button")
	}
	if strings.Contains(body, "/admin/matches/5/log") {
		t.Error("the log link must be hidden when the file does not exist")
	}
	if n := strings.Count(body, "data-command-result"); n != 1 {
		t.Errorf("data-command-result appears %d times", n)
	}
	if !strings.Contains(body, `href="/admin"`) {
		t.Error("the user menu links the dashboard")
	}
	if !strings.Contains(body, "data-match-id=\"5\"") {
		t.Error("match page root lacks data-match-id")
	}
}

func TestMatchPageReloadOnlyOnCurrentMap(t *testing.T) {
	e := newMatchTestEnv(t)
	e.st.maps[5] = append(e.st.maps[5], store.Map{ID: 12, MatchID: 5, Name: "de_mirage"})
	body := e.do("GET", "/matches/5", "admin", nil).Body.String()
	if n := strings.Count(body, "Reload to start of round 4"); n != 1 {
		t.Errorf("reload button appears %d times, want once (the rounds belong to map 10)", n)
	}
}

func TestMatchPageNoDuplicateEdit(t *testing.T) {
	e := newMatchTestEnv(t)
	body := e.do("GET", "/matches/6", "admin", nil).Body.String()
	if n := strings.Count(body, `href="/admin/matches/6/edit"`); n != 1 {
		t.Errorf("edit link appears %d times", n)
	}
	if strings.Contains(body, "Edit match") {
		t.Error("the extra Edit match link must not show next to Edit")
	}

	m := e.st.matches[1]
	m.Status = store.StatusEndMatch
	e.st.matches[1] = m
	body = e.do("GET", "/matches/6", "admin", nil).Body.String()
	if n := strings.Count(body, `href="/admin/matches/6/edit"`); n != 1 {
		t.Errorf("finished match: edit link appears %d times", n)
	}
}

func TestMatchPageStartServerPreselected(t *testing.T) {
	e := newMatchTestEnv(t)
	idle := e.st.matches[1]
	idle.ServerID = matchI64(2)
	e.st.matches[1] = idle
	body := e.do("GET", "/matches/6", "admin", nil).Body.String()
	if !strings.Contains(body, "Use the assigned server") || !strings.Contains(body, `<option value="2" selected>`) {
		t.Error("the assigned server must be preselected in the start picker")
	}
}

func TestListPagesCarryPage(t *testing.T) {
	e := newMatchTestEnv(t)
	if body := e.do("GET", "/", "", nil).Body.String(); !strings.Contains(body, `data-page="1"`) {
		t.Error("home lacks data-page")
	}
	if body := e.do("GET", "/matches?page=1", "", nil).Body.String(); !strings.Contains(body, `data-page="1"`) {
		t.Error("list lacks data-page")
	}
}

func TestListPageIsClamped(t *testing.T) {
	e := newMatchTestEnv(t)
	e.do("GET", "/matches?page=9223372036854775807", "", nil)
	if e.st.filter.Page != maxListPage {
		t.Errorf("page = %d", e.st.filter.Page)
	}
	e.do("GET", "/matches?q="+url.QueryEscape(strings.Repeat("æ", 150)), "", nil)
	if got := []rune(e.st.filter.Search); len(got) != 100 {
		t.Errorf("search has %d runes", len(got))
	}
}

func TestStatsPageNames(t *testing.T) {
	e := newMatchTestEnv(t)
	body := e.do("GET", "/stats", "", nil).Body.String()
	if strings.Contains(body, ">MVP<") || !strings.Contains(body, ">Kills/match<") {
		t.Error("stats column must be Kills/match")
	}
}

func TestUsersListShowsNoAccess(t *testing.T) {
	st := &fakeUsers{}
	e := userEntity(st)
	rows, err := e.Rows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Cells[0].Text] = r.Cells[2].Text
	}
	if got["viewer"] != "No access" || got["boss"] != "Super admin" || got["gone"] != "Inactive" {
		t.Errorf("roles = %v", got)
	}
	fields, _ := e.Fields(context.Background(), false)
	for _, f := range fields {
		if f.Name == "is_super_admin" && !strings.Contains(f.Help, "Only super admins can log in") {
			t.Errorf("help = %q", f.Help)
		}
	}
}

func TestPasswordWordsPlain(t *testing.T) {
	for _, w := range matchPasswordWords {
		for _, r := range w {
			if r < 'a' || r > 'z' {
				t.Errorf("word %q has a character outside a-z", w)
			}
		}
	}
}

type fakeUsers struct{ userStore }

func (fakeUsers) ListUsers(context.Context) ([]store.User, error) {
	return []store.User{
		{ID: 1, Username: "viewer", IsActive: true},
		{ID: 2, Username: "boss", IsActive: true, IsSuperAdmin: true},
		{ID: 3, Username: "gone"},
	}, nil
}

func TestSeasonDefaultsUseTheConfiguredZone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 UTC on the 21st is already the 22nd in Copenhagen.
	instant := time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
	got := seasonDefaults(func() time.Time { return instant.In(loc) })
	if got.Get("start") != "2026-10-22" || got.Get("end") != "2026-10-22" || got.Get("active") != "1" {
		t.Errorf("defaults = %v", got)
	}
	e := &entity{Defaults: url.Values{"start": {"stale"}}, DefaultsFn: func() url.Values { return seasonDefaults(func() time.Time { return instant.In(loc) }) }}
	if e.DefaultsFn().Get("start") == "stale" {
		t.Error("DefaultsFn must win over Defaults")
	}
}
