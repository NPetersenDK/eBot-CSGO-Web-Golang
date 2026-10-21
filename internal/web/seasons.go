package web

import (
	"context"
	"net/url"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type seasonStore interface {
	ListSeasons(ctx context.Context) ([]store.Season, error)
	GetSeason(ctx context.Context, id int64) (*store.Season, error)
	CreateSeason(ctx context.Context, in store.SeasonInput) (int64, error)
	UpdateSeason(ctx context.Context, id int64, in store.SeasonInput) error
	DeleteSeason(ctx context.Context, id int64) error
	DeactivateSeason(ctx context.Context, id int64) error
}

const dateLayout = "2006-01-02"

func validDate(v string, _ url.Values) string {
	if _, err := time.Parse(dateLayout, v); err != nil {
		return "Use the form YYYY-MM-DD."
	}
	return ""
}

func dayPart(s string) string {
	if len(s) >= len(dateLayout) {
		return s[:len(dateLayout)]
	}
	return s
}

func seasonEntity(st seasonStore) *entity {
	fields := []field{
		{Name: "name", Label: "Name", Type: "text", Required: true, Max: 50},
		{Name: "event", Label: "Event", Type: "text", Required: true, Max: 50},
		{Name: "start", Label: "Start", Type: "text", Required: true, Help: "YYYY-MM-DD", Validate: validDate},
		{Name: "end", Label: "End", Type: "text", Required: true, Help: "YYYY-MM-DD",
			Validate: func(v string, f url.Values) string {
				if msg := validDate(v, f); msg != "" {
					return msg
				}
				s, errS := time.Parse(dateLayout, f.Get("start"))
				e, _ := time.Parse(dateLayout, v)
				if errS == nil && e.Before(s) {
					return "The end must not be before the start."
				}
				return ""
			}},
		{Name: "link", Label: "Link", Type: "text", Max: 100, Validate: validURL},
		{Name: "logo", Label: "Season logo", Type: "text", Max: 255, Help: "Path or URL of the logo image."},
		{Name: "active", Label: "Active", Type: "checkbox"},
	}
	input := func(v url.Values) store.SeasonInput {
		return store.SeasonInput{
			Name: v.Get("name"), Event: v.Get("event"), Start: v.Get("start"), End: v.Get("end"),
			Link: v.Get("link"), Logo: v.Get("logo"), Active: v.Get("active") != "",
		}
	}
	today := time.Now().Format(dateLayout)
	return &entity{
		Slug:       "seasons",
		Singular:   "Season",
		Plural:     "Seasons management",
		NewLabel:   "Create season",
		Columns:    []string{"#ID", "Name", "Start", "End", "Link", "Status"},
		Empty:      "No seasons yet.",
		Defaults:   url.Values{"start": {today}, "end": {today}, "active": {"1"}},
		Fields:     func(context.Context, bool) ([]field, error) { return fields, nil },
		Deactivate: st.DeactivateSeason,
		Rows: func(ctx context.Context) ([]listRow, error) {
			list, err := st.ListSeasons(ctx)
			if err != nil {
				return nil, err
			}
			rows := make([]listRow, 0, len(list))
			for _, s := range list {
				rows = append(rows, listRow{
					ID: s.ID,
					Cells: []listCell{
						{Text: int64Text(s.ID)}, {Text: s.Name}, {Text: dayPart(s.Start)}, {Text: dayPart(s.End)},
						{Text: s.Link, Link: s.Link}, activeBadge(s.Active),
					},
					Active:        s.Active,
					CanDeactivate: s.Active,
					CanDelete:     true,
					DeleteText:    "Delete season " + s.Name + "?",
				})
			}
			return rows, nil
		},
		Load: func(ctx context.Context, id int64) (url.Values, error) {
			s, err := st.GetSeason(ctx, id)
			if err != nil {
				return nil, err
			}
			return url.Values{
				"name": {s.Name}, "event": {s.Event}, "start": {dayPart(s.Start)}, "end": {dayPart(s.End)},
				"link": {s.Link}, "logo": {s.Logo}, "active": {boolText(s.Active)},
			}, nil
		},
		Create: func(ctx context.Context, v url.Values) (string, error) {
			_, err := st.CreateSeason(ctx, input(v))
			return "", err
		},
		Update: func(ctx context.Context, id int64, v url.Values) error {
			return st.UpdateSeason(ctx, id, input(v))
		},
		Delete: st.DeleteSeason,
	}
}

func validURL(v string, _ url.Values) string {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "Enter a full http or https address."
	}
	return ""
}
