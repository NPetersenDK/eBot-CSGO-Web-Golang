package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const maxAliases = 5

type playerMapRow struct {
	store.PlayerMatch
	Checked bool
}

type playerPage struct {
	SteamID string
	Name    string
	// Aliases are other names the player has used, newest first.
	Aliases []string
	Rows    []playerMapRow
	Totals  store.PlayerTotals
	// Filtered is true when the totals cover only the chosen maps.
	Filtered bool
	// Chosen is the number of maps in the totals, Total the number played.
	Chosen int
	Total  int
}

func (p playerPage) Title() string { return "Player statistics: " + p.Name }

// player shows one player summed over all maps, or over the maps picked with
// ?row=<players.id> (repeated).
func (h *matchHandlers) player(w http.ResponseWriter, r *http.Request) {
	steam := r.PathValue("steamid")
	all, err := h.st.ListPlayerMatches(r.Context(), steam)
	if errors.Is(err, store.ErrNotFound) || (err == nil && len(all) == 0) {
		h.app.NotFound(w, r)
		return
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}

	picked := map[int64]bool{}
	for _, v := range r.URL.Query()["row"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			picked[id] = true
		}
	}
	var chosen []store.PlayerMatch
	for _, m := range all {
		if picked[m.ID] {
			chosen = append(chosen, m)
		}
	}
	filtered := len(chosen) > 0
	if !filtered {
		chosen = all
	}

	page := playerPage{
		SteamID:  steam,
		Name:     all[0].Nickname,
		Totals:   store.SumPlayerMatches(chosen),
		Filtered: filtered,
		Chosen:   len(chosen),
		Total:    len(all),
	}
	seen := map[string]bool{page.Name: true}
	for _, m := range all {
		if m.Nickname != "" && !seen[m.Nickname] && len(page.Aliases) < maxAliases {
			seen[m.Nickname] = true
			page.Aliases = append(page.Aliases, m.Nickname)
		}
	}
	for _, m := range all {
		page.Rows = append(page.Rows, playerMapRow{PlayerMatch: m, Checked: filtered && picked[m.ID]})
	}
	h.app.Render(w, r, "player", page)
}
