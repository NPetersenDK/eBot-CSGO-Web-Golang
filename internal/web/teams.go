package web

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type teamStore interface {
	ListTeams(ctx context.Context) ([]store.Team, error)
	GetTeam(ctx context.Context, id int64) (*store.Team, error)
	CreateTeam(ctx context.Context, in store.TeamInput) (int64, error)
	UpdateTeam(ctx context.Context, id int64, in store.TeamInput) error
	DeleteTeam(ctx context.Context, id int64) error
}

// teamFlags is the country list from the old app.yml (flag_team_en).
var teamFlags = []option{
	{"DZ", "Algeria"},
	{"AR", "Argentina"},
	{"AU", "Australia"},
	{"AT", "Austria"},
	{"BY", "Belarus"},
	{"BE", "Belgium"},
	{"BR", "Brazil"},
	{"BG", "Bulgaria"},
	{"CA", "Canada"},
	{"CL", "Chile"},
	{"CN", "China"},
	{"CC", "Cis"},
	{"HR", "Croatia"},
	{"CZ", "Czech Republic"},
	{"DK", "Denmark"},
	{"EE", "Estonia"},
	{"EU", "Europe"},
	{"FI", "Finland"},
	{"FR", "France"},
	{"DE", "Germany"},
	{"GR", "Greece"},
	{"HK", "Hong Kong"},
	{"HU", "Hungary"},
	{"IS", "Iceland"},
	{"IN", "India"},
	{"ID", "Indonesia"},
	{"IR", "Iran"},
	{"IE", "Ireland"},
	{"IL", "Israel"},
	{"IT", "Italy"},
	{"JP", "Japan"},
	{"KZ", "Kazakhstan"},
	{"KR", "Korea"},
	{"LV", "Latvia"},
	{"LY", "Libya"},
	{"LT", "Lithuania"},
	{"LU", "Luxembourg"},
	{"MK", "Macedonia"},
	{"MY", "Malaysia"},
	{"MX", "Mexico"},
	{"MN", "Mongolia"},
	{"MA", "Morocco"},
	{"NL", "Netherlands"},
	{"NZ", "New Zealand"},
	{"NO", "Norway"},
	{"PK", "Pakistan"},
	{"PE", "Peru"},
	{"PH", "Philippines"},
	{"PL", "Poland"},
	{"PT", "Portugal"},
	{"RO", "Romania"},
	{"RU", "Russian Federation"},
	{"SA", "Saudi Arabia"},
	{"RS", "Serbia"},
	{"SG", "Singapore"},
	{"SK", "Slovakia"},
	{"SI", "Slovenia"},
	{"ZA", "South Africa"},
	{"ES", "Spain"},
	{"SE", "Sweden"},
	{"CH", "Switzerland"},
	{"TW", "Taiwan"},
	{"TH", "Thailand"},
	{"TN", "Tunisia"},
	{"TR", "Turkey"},
	{"UA", "Ukraine"},
	{"AE", "United Arab Emirates"},
	{"GB", "United Kingdom"},
	{"US", "United States"},
	{"VE", "Venezuela"},
	{"VN", "Vietnam"},
}

// withStoredFlags adds flags that teams already use but the list lacks, so
// editing a team made by the PHP panel does not change its flag.
func withStoredFlags(ctx context.Context, st teamStore) ([]option, error) {
	teams, err := st.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, o := range teamFlags {
		known[o.Value] = true
	}
	out := append([]option(nil), teamFlags...)
	for _, t := range teams {
		if t.Flag != "" && !known[t.Flag] {
			known[t.Flag] = true
			out = append(out, option{t.Flag, t.Flag})
		}
	}
	return out, nil
}

func teamEntity(st interface {
	teamStore
	ListSeasons(ctx context.Context) ([]store.Season, error)
}) *entity {
	input := func(v url.Values) store.TeamInput {
		in := store.TeamInput{Name: v.Get("name"), ShortName: v.Get("short_name"), Flag: v.Get("flag"), Link: v.Get("link")}
		for _, s := range v["seasons"] {
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				in.SeasonIDs = append(in.SeasonIDs, id)
			}
		}
		return in
	}
	return &entity{
		Slug:     "teams",
		Singular: "Team",
		Plural:   "Team management",
		NewLabel: "Create new team",
		Columns:  []string{"#ID", "Flag", "Name", "Short name", "Team link"},
		Empty:    "No teams yet.",
		Fields: func(ctx context.Context, isEdit bool) ([]field, error) {
			seasons, err := seasonOptions(ctx, st)
			if err != nil {
				return nil, err
			}
			name := field{Name: "name", Label: "Name", Type: "text", Required: true, Max: 25,
				Validate: func(v string, _ url.Values) string {
					if err := store.CheckTeamName(v); err != nil {
						return err.Error()
					}
					return ""
				}}
			flags := teamFlags
			if isEdit {
				// A stored name is only checked by the store when it changed.
				name.Max, name.Validate = 0, nil
				if flags, err = withStoredFlags(ctx, st); err != nil {
					return nil, err
				}
			}
			return []field{
				name,
				{Name: "short_name", Label: "Short name", Type: "text", Required: true, Max: 50},
				{Name: "flag", Label: "Flag", Type: "select", Required: true, Options: flags},
				{Name: "link", Label: "Team link", Type: "text", Max: 100, Validate: validURL},
				{Name: "seasons", Label: "Seasons", Type: "multiselect", Options: seasons, Help: "Hold Ctrl or Cmd to select several."},
			}, nil
		},
		Rows: func(ctx context.Context) ([]listRow, error) {
			list, err := st.ListTeams(ctx)
			if err != nil {
				return nil, err
			}
			rows := make([]listRow, 0, len(list))
			for _, t := range list {
				rows = append(rows, listRow{
					ID: t.ID,
					Cells: []listCell{
						{Text: int64Text(t.ID)}, {Text: t.Flag, Badge: "muted", Mono: true}, {Text: t.Name}, {Text: t.ShortName},
						{Text: t.Link, Link: t.Link},
					},
					CanDelete:  true,
					DeleteText: "Delete team " + t.Name + "?",
				})
			}
			return rows, nil
		},
		Load: func(ctx context.Context, id int64) (url.Values, error) {
			t, err := st.GetTeam(ctx, id)
			if err != nil {
				return nil, err
			}
			v := url.Values{"name": {t.Name}, "short_name": {t.ShortName}, "flag": {t.Flag}, "link": {t.Link}}
			for _, s := range t.SeasonIDs {
				v.Add("seasons", int64Text(s))
			}
			return v, nil
		},
		Create: func(ctx context.Context, v url.Values) (string, error) {
			_, err := st.CreateTeam(ctx, input(v))
			return "", err
		},
		Update: func(ctx context.Context, id int64, v url.Values) error {
			in := input(v)
			err := st.UpdateTeam(ctx, id, in)
			if errors.Is(err, store.ErrInvalid) {
				if nameErr := store.CheckTeamName(in.Name); nameErr != nil {
					return fieldErrors{"name": nameErr.Error()}
				}
			}
			return err
		},
		Delete: st.DeleteTeam,
	}
}
