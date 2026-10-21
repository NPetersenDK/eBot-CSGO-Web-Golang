package store

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The bot pastes these values into console commands without escaping them:
//
//	sv_password "<password>"      mp_teamname_1 "<team name>"
//	mp_teamflag_1 "<flag>"        exec <rules>.cfg        changelevel <map>
//
// so a quote, a semicolon or a line break in one of them runs a second command
// on the game server. The checks below keep those characters out.
var (
	rulesPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)
	mapPattern   = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,64}$`)
	flagPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,8}$`)
)

const (
	maxTeamName = 25
	maxPassword = 50
)

// CheckRules validates the rules file name (without ".cfg").
func CheckRules(s string) error {
	if !rulesPattern.MatchString(s) {
		return invalid("Use letters, digits, dot, dash and underscore only.")
	}
	return nil
}

// CheckMapName validates a map name. "tba", "CurrentMap", "CurrentMap-<map>"
// and workshop names all fit.
func CheckMapName(s string) error {
	if !mapPattern.MatchString(s) {
		return invalid("A map name may only hold letters, digits, dot, dash, slash and underscore.")
	}
	return nil
}

// CheckFlag validates a team flag. An empty flag is fine.
func CheckFlag(s string) error {
	if s != "" && !flagPattern.MatchString(s) {
		return invalid("A flag may only hold letters, digits, dash and underscore, up to 8 characters.")
	}
	return nil
}

// CheckSafeText validates free text the bot puts inside quotes: no quote,
// semicolon, backslash or control character, and at most max characters.
func CheckSafeText(label, s string, max int) error {
	if utf8.RuneCountInString(s) > max {
		return invalid(fmt.Sprintf("%s: at most %d characters.", label, max))
	}
	if i := strings.IndexFunc(s, func(r rune) bool {
		return r == '"' || r == ';' || r == '\\' || unicode.IsControl(r)
	}); i >= 0 {
		return invalid(label + ": quotes, semicolons, backslashes and control characters are not allowed.")
	}
	return nil
}

// CheckTeamName validates a team name.
func CheckTeamName(s string) error { return CheckSafeText("Team name", s, maxTeamName) }

// CheckMatchPassword validates the game server password.
func CheckMatchPassword(s string) error { return CheckSafeText("Password", s, maxPassword) }

// matchSnapshot holds the stored values of the fields that reach the console.
type matchSnapshot struct {
	TeamAName, TeamBName, TeamAFlag, TeamBFlag string
	Rules, Password, MapName                   string
}

// validateMatch checks the values that changed. A value that is already in the
// database is accepted as it is, so a match made by the PHP panel with an odd
// team name can still be edited without changing that name.
func validateMatch(in MatchInput, old matchSnapshot) error {
	check := func(v, was string, f func(string) error) error {
		if v == was {
			return nil
		}
		return f(v)
	}
	mapName := ""
	if len(in.Maps) > 0 {
		mapName = in.Maps[0].Name
	}
	type field struct {
		v, was string
		f      func(string) error
	}
	fields := []field{
		{in.Rules, old.Rules, CheckRules},
		{in.ConfigPassword, old.Password, CheckMatchPassword},
		{mapName, old.MapName, CheckMapName},
	}
	// A team picked from the list gets its name and flag from the teams table,
	// not from the input, so only free text is checked.
	if in.TeamAID == nil {
		fields = append(fields, field{in.TeamAName, old.TeamAName, CheckTeamName}, field{in.TeamAFlag, old.TeamAFlag, CheckFlag})
	}
	if in.TeamBID == nil {
		fields = append(fields, field{in.TeamBName, old.TeamBName, CheckTeamName}, field{in.TeamBFlag, old.TeamBFlag, CheckFlag})
	}
	for _, c := range fields {
		if err := check(c.v, c.was, c.f); err != nil {
			return err
		}
	}
	return nil
}
