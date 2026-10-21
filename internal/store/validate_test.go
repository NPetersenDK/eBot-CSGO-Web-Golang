package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckSafeText(t *testing.T) {
	for _, bad := range []string{`x"; rcon_password pwned; say "`, "a;b", `a"b`, `a\b`, "a\nb", "a\rb", "a\x00b", "tab\there"} {
		if err := CheckSafeText("Name", bad, 100); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"Team Alpha", "eSport-Brande 2", "Sdr. Omme Esport", "Æ Ø Å ñ", "A_b-c (1)", "'quoted'", ""} {
		if err := CheckSafeText("Name", good, 100); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	if err := CheckSafeText("Name", strings.Repeat("a", 26), 25); !errors.Is(err, ErrInvalid) {
		t.Error("too long accepted")
	}
	if err := CheckSafeText("Name", strings.Repeat("æ", 25), 25); err != nil {
		t.Errorf("25 characters must fit even when multi-byte: %v", err)
	}
}

func TestCheckRulesMapFlag(t *testing.T) {
	for _, bad := range []string{"", "rules; quit", "a b", "x/y", "../x", `r"`, "a\nb"} {
		if CheckRules(bad) == nil {
			t.Errorf("rules %q accepted", bad)
		}
	}
	for _, good := range []string{"rules", "gamemode_competitive", "esl5on5", "my.rules-2"} {
		if err := CheckRules(good); err != nil {
			t.Errorf("rules %q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "de_dust2; quit", "de dust", "de_dust2\n", `a"b`, strings.Repeat("a", 65)} {
		if CheckMapName(bad) == nil {
			t.Errorf("map %q accepted", bad)
		}
	}
	for _, good := range []string{"de_dust2", "tba", "CurrentMap", "CurrentMap-de_dust2", "workshop/123/de_x", "de_aztec_classic"} {
		if err := CheckMapName(good); err != nil {
			t.Errorf("map %q: %v", good, err)
		}
	}
	for _, bad := range []string{`D"K`, "DK; x", "toolongflag"} {
		if CheckFlag(bad) == nil {
			t.Errorf("flag %q accepted", bad)
		}
	}
	for _, good := range []string{"", "DK", "se", "EU-1"} {
		if err := CheckFlag(good); err != nil {
			t.Errorf("flag %q: %v", good, err)
		}
	}
}

func TestValidateMatchOnlyChecksWhatChanged(t *testing.T) {
	bad := `Evil"; quit`
	in := MatchInput{TeamAName: bad, TeamBName: "Bravo", Rules: "rules", Maps: []MapInput{{Name: "de_dust2"}}}

	if err := validateMatch(in, matchSnapshot{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a new bad team name must be refused: %v", err)
	}
	// The PHP panel stored it. Editing something else must still work.
	old := matchSnapshot{TeamAName: bad, TeamBName: "Bravo", Rules: "rules", MapName: "de_dust2"}
	if err := validateMatch(in, old); err != nil {
		t.Fatalf("an unchanged stored value must be accepted: %v", err)
	}
	in.TeamAName = `Other"; quit`
	if err := validateMatch(in, old); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changing it to another bad value must be refused: %v", err)
	}
	// A team from the list brings its own name.
	id := int64(3)
	in.TeamAID = &id
	if err := validateMatch(in, old); err != nil {
		t.Fatalf("names of listed teams are not checked: %v", err)
	}

	for name, in := range map[string]MatchInput{
		"map":      {Rules: "rules", Maps: []MapInput{{Name: "de_dust2; quit"}}},
		"rules":    {Rules: "x; sv_cheats 1; exec x", Maps: []MapInput{{Name: "de_dust2"}}},
		"password": {Rules: "rules", ConfigPassword: `pw"; quit`, Maps: []MapInput{{Name: "de_dust2"}}},
		"flag":     {Rules: "rules", TeamAFlag: "D K", Maps: []MapInput{{Name: "de_dust2"}}},
	} {
		if err := validateMatch(in, matchSnapshot{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMatchesRefuseConsoleInjection(t *testing.T) {
	s := matchesTestStore(t)
	ctx := context.Background()
	valid := func() MatchInput {
		return MatchInput{TeamAName: "Alpha", TeamBName: "Bravo", MaxRound: 12, Rules: "rules", MapSelectionMode: "normal", Maps: []MapInput{{Name: "de_dust2", PickedBy: "default"}}, Side: "ct"}
	}

	in := valid()
	in.TeamAName = `x"; rcon_password pwned; say "`
	if _, err := s.CreateMatch(ctx, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create with a bad team name: %v", err)
	}

	id, err := s.CreateMatch(ctx, valid())
	if err != nil {
		t.Fatal(err)
	}
	// Written by the PHP panel: stored as it is, editing must keep working.
	mustExec(t, s, `UPDATE matchs SET team_a_name = 'Odd"; name', config_password = 'p;w' WHERE id = ?`, id)
	edit := valid()
	edit.TeamAName, edit.ConfigPassword = `Odd"; name`, "p;w"
	edit.Rules = "esl5on5"
	if err := s.UpdateMatch(ctx, id, edit); err != nil {
		t.Fatalf("editing a match with stored odd values: %v", err)
	}
	m, _ := s.GetMatch(ctx, id)
	if m.TeamAName != `Odd"; name` || m.Rules != "esl5on5" {
		t.Errorf("after edit: %q %q", m.TeamAName, m.Rules)
	}

	edit.Maps = []MapInput{{Name: "de_mirage; quit"}}
	if err := s.UpdateMatch(ctx, id, edit); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a new bad map must be refused: %v", err)
	}
	m, _ = s.GetMatch(ctx, id)
	if m.CurrentMap == nil || m.CurrentMap.Name != "de_dust2" {
		t.Errorf("a refused edit changed the map: %+v", m.CurrentMap)
	}
}
