package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type fakeTeams struct {
	teams  []store.Team
	update store.TeamInput
	err    error
}

func (f *fakeTeams) ListTeams(context.Context) ([]store.Team, error)            { return f.teams, nil }
func (f *fakeTeams) ListSeasons(context.Context) ([]store.Season, error)        { return nil, nil }
func (f *fakeTeams) GetTeam(context.Context, int64) (*store.Team, error)        { return &f.teams[0], nil }
func (f *fakeTeams) CreateTeam(context.Context, store.TeamInput) (int64, error) { return 1, nil }
func (f *fakeTeams) DeleteTeam(context.Context, int64) error                    { return nil }
func (f *fakeTeams) UpdateTeam(_ context.Context, _ int64, in store.TeamInput) error {
	f.update = in
	return f.err
}

func fieldNamed(t *testing.T, fields []field, name string) field {
	t.Helper()
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %q", name)
	return field{}
}

func TestTeamNameValidation(t *testing.T) {
	e := teamEntity(&fakeTeams{})
	fields, err := e.Fields(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	name := fieldNamed(t, fields, "name")
	for v, ok := range map[string]bool{
		"Alpha": true, `Al"pha`: false, "A;B": false, `A\B`: false, "A\nB": false, strings.Repeat("x", 26): false, strings.Repeat("x", 25): true,
	} {
		msg := validateField(name, url.Values{"name": {v}})
		if (msg == "") != ok {
			t.Errorf("name %q: message %q", v, msg)
		}
	}
}

func TestTeamEditKeepsLegacyValues(t *testing.T) {
	st := &fakeTeams{teams: []store.Team{{ID: 1, Name: `Old"name`, Flag: "XX"}, {ID: 2, Name: "B", Flag: "DK"}}}
	e := teamEntity(st)
	fields, err := e.Fields(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if msg := validateField(fieldNamed(t, fields, "name"), url.Values{"name": {`Old"name`}}); msg != "" {
		t.Errorf("edit rejected a stored name: %q", msg)
	}
	flag := fieldNamed(t, fields, "flag")
	if msg := validateField(flag, url.Values{"flag": {"XX"}}); msg != "" {
		t.Errorf("legacy flag not selectable: %q", msg)
	}
	count := 0
	for _, o := range flag.Options {
		if o.Value == "XX" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("legacy flag listed %d times", count)
	}
}

func TestTeamUpdateInvalidNameIsFieldError(t *testing.T) {
	st := &fakeTeams{err: store.ErrInvalid}
	e := teamEntity(st)
	err := e.Update(context.Background(), 1, url.Values{"name": {`a"b`}, "short_name": {"x"}, "flag": {"DK"}})
	fe, ok := err.(fieldErrors)
	if !ok || fe["name"] == "" {
		t.Fatalf("err = %v", err)
	}
}

func TestServerCreateTextValidation(t *testing.T) {
	e := serverEntity(&fakeServers{})
	fields, _ := e.Fields(context.Background(), false)
	for _, name := range []string{"hostname", "tv_ip"} {
		f := fieldNamed(t, fields, name)
		if msg := validateField(f, url.Values{name: {`x"; quit`}}); msg == "" {
			t.Errorf("%s accepted a quote", name)
		}
		if msg := validateField(f, url.Values{name: {"Server 1"}}); msg != "" {
			t.Errorf("%s rejected a plain value: %q", name, msg)
		}
	}
}

type fakeServers struct{}

func (fakeServers) ListServers(context.Context) ([]store.Server, error) { return nil, nil }
func (fakeServers) GetServer(context.Context, int64) (*store.Server, error) {
	return &store.Server{RCON: "x"}, nil
}
func (fakeServers) CreateServers(context.Context, store.ServerCreate) (int, error) { return 1, nil }
func (fakeServers) UpdateServer(context.Context, int64, store.ServerInput) error {
	return store.ErrInvalid
}
func (fakeServers) DeleteServer(context.Context, int64) error { return nil }
