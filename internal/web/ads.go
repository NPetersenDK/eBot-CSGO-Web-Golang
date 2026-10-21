package web

import (
	"context"
	"net/url"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type adStore interface {
	ListAds(ctx context.Context) ([]store.Ad, error)
	GetAd(ctx context.Context, id int64) (*store.Ad, error)
	CreateAd(ctx context.Context, in store.AdInput) (int64, error)
	UpdateAd(ctx context.Context, id int64, in store.AdInput) error
	DeleteAd(ctx context.Context, id int64) error
	DeactivateAd(ctx context.Context, id int64) error
}

func seasonOptions(ctx context.Context, st interface {
	ListSeasons(ctx context.Context) ([]store.Season, error)
}) ([]option, error) {
	seasons, err := st.ListSeasons(ctx)
	if err != nil {
		return nil, err
	}
	opts := make([]option, 0, len(seasons))
	for _, s := range seasons {
		opts = append(opts, option{int64Text(s.ID), s.Name})
	}
	return opts, nil
}

func adEntity(st interface {
	adStore
	ListSeasons(ctx context.Context) ([]store.Season, error)
}) *entity {
	input := func(v url.Values) store.AdInput {
		sid, _ := strconv.ParseInt(v.Get("season_id"), 10, 64)
		return store.AdInput{SeasonID: sid, Message: v.Get("message"), Active: v.Get("active") != ""}
	}
	return &entity{
		Slug:     "ads",
		Singular: "Advert",
		Plural:   "Advertising management",
		NewLabel: "Add new advert",
		Columns:  []string{"#ID", "Message", "Season", "Status"},
		Empty:    "No adverts yet.",
		Defaults: url.Values{"active": {"1"}},
		Fields: func(ctx context.Context, _ bool) ([]field, error) {
			opts, err := seasonOptions(ctx, st)
			if err != nil {
				return nil, err
			}
			return []field{
				{Name: "season_id", Label: "Season", Type: "select", Required: true, Options: opts},
				{Name: "message", Label: "Message", Type: "textarea", Required: true, Max: 1000},
				{Name: "active", Label: "Active", Type: "checkbox"},
			}, nil
		},
		Deactivate: st.DeactivateAd,
		Rows: func(ctx context.Context) ([]listRow, error) {
			list, err := st.ListAds(ctx)
			if err != nil {
				return nil, err
			}
			rows := make([]listRow, 0, len(list))
			for _, ad := range list {
				rows = append(rows, listRow{
					ID:            ad.ID,
					Cells:         []listCell{{Text: int64Text(ad.ID)}, {Text: ad.Message}, {Text: ad.Season}, activeBadge(ad.Active)},
					Active:        ad.Active,
					CanDeactivate: ad.Active,
					CanDelete:     true,
					DeleteText:    "Delete this advert?",
				})
			}
			return rows, nil
		},
		Load: func(ctx context.Context, id int64) (url.Values, error) {
			ad, err := st.GetAd(ctx, id)
			if err != nil {
				return nil, err
			}
			return url.Values{"season_id": {int64Text(ad.SeasonID)}, "message": {ad.Message}, "active": {boolText(ad.Active)}}, nil
		},
		Create: func(ctx context.Context, v url.Values) (string, error) {
			_, err := st.CreateAd(ctx, input(v))
			return "", err
		},
		Update: func(ctx context.Context, id int64, v url.Values) error { return st.UpdateAd(ctx, id, input(v)) },
		Delete: st.DeleteAd,
	}
}
