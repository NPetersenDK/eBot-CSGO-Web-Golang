package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

var validMapModes = map[string]bool{
	"bo2": true, "bo3_modea": true, "bo3_modeb": true, "normal": true,
}

const listPageSize = 100

func (s *server) listMatches(w http.ResponseWriter, r *http.Request) {
	limit := min(max(queryInt(r, "limit", 50), 1), 500)
	offset := max(queryInt(r, "offset", 0), 0)

	filter := -1
	if sv := r.URL.Query().Get("status"); sv != "" {
		st, err := strconv.Atoi(sv)
		if err != nil {
			writeError(w, http.StatusBadRequest, "status must be an integer")
			return
		}
		filter = st
	}

	f := store.MatchFilter{Archived: filter == store.StatusArchived, PerPage: listPageSize}
	out := []matchJSON{}
	skipped := 0
	for page := 1; len(out) < limit; page++ {
		f.Page = page
		list, pg, err := s.store.ListMatches(r.Context(), f)
		if err != nil {
			storeError(w, err, "match not found")
			return
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].ID > list[j].ID })
		for i := range list {
			if filter >= 0 && list[i].Status != filter {
				continue
			}
			if skipped < offset {
				skipped++
				continue
			}
			mj := toMatch(&list[i])
			mj.ConfigPassword = ""
			out = append(out, mj)
			if len(out) == limit {
				break
			}
		}
		if !pg.HasNext() {
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.writeMatch(w, r.Context(), http.StatusOK, id)
}

func (s *server) writeMatch(w http.ResponseWriter, ctx context.Context, code int, id int64) {
	m, err := s.store.GetMatch(ctx, id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	writeJSON(w, code, toMatch(m))
}

func (s *server) createMatch(w http.ResponseWriter, r *http.Request) {
	var req createMatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.MaxRound == 0 {
		req.MaxRound = 30
	}
	if req.Rules == "" {
		writeError(w, http.StatusBadRequest, "rules is required")
		return
	}
	if req.MapSelectionMode == "" {
		req.MapSelectionMode = "normal"
	}
	if !validMapModes[req.MapSelectionMode] {
		writeError(w, http.StatusBadRequest, "map_selection_mode must be one of bo2, bo3_modea, bo3_modeb, normal")
		return
	}
	maps := req.Maps
	if len(maps) == 0 {
		maps = []string{"tba"}
	}
	if len(maps) > 1 {
		writeError(w, http.StatusBadRequest, "maps takes one map, the bot creates the other maps of a series")
		return
	}
	if req.Side != "" && req.Side != "ct" && req.Side != "t" {
		writeError(w, http.StatusBadRequest, "side must be ct or t")
		return
	}
	ctx := r.Context()

	checks := []error{
		store.CheckRules(req.Rules),
		store.CheckMapName(maps[0]),
		store.CheckMatchPassword(req.ConfigPassword),
	}
	for _, t := range []struct {
		id         *int64
		name, flag *string
	}{
		{req.TeamAID, &req.TeamAName, &req.TeamAFlag},
		{req.TeamBID, &req.TeamBName, &req.TeamBFlag},
	} {
		if t.id != nil {
			team, err := s.store.GetTeam(ctx, *t.id)
			if err == nil {
				*t.name, *t.flag = team.Name, team.Flag
				continue
			}
			if !isNotFound(err) {
				storeError(w, err, "team not found")
				return
			}
		}
		checks = append(checks, store.CheckTeamName(*t.name), store.CheckFlag(*t.flag))
	}
	for _, err := range checks {
		if err != nil {
			storeError(w, err, "")
			return
		}
	}

	serverID := req.ServerID
	if serverID != nil {
		if _, err := s.store.GetServer(ctx, *serverID); err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusBadRequest, "server_id does not exist")
				return
			}
			storeError(w, err, "server not found")
			return
		}
	} else if req.Start {
		free, err := s.store.FreeServers(ctx)
		if err != nil {
			storeError(w, err, "server not found")
			return
		}
		if len(free) == 0 {
			writeError(w, http.StatusConflict, "no free server available to start the match")
			return
		}
		sort.SliceStable(free, func(i, j int) bool { return free[i].Hostname < free[j].Hostname })
		serverID = &free[0].ID
	}

	in := store.MatchInput{
		SeasonID: req.SeasonID,
		TeamAID:  req.TeamAID, TeamBID: req.TeamBID,
		TeamAName: req.TeamAName, TeamBName: req.TeamBName,
		TeamAFlag: req.TeamAFlag, TeamBFlag: req.TeamBFlag,
		MaxRound:         req.MaxRound,
		Rules:            req.Rules,
		ConfigPassword:   req.ConfigPassword,
		ConfigFullScore:  deref(req.ConfigFullScore),
		ConfigOT:         deref(req.ConfigOt),
		ConfigKnifeRound: deref(req.ConfigKnifeRound),
		ConfigSwitchAuto: deref(req.ConfigSwitchAuto),
		MapSelectionMode: req.MapSelectionMode,
	}
	if deref(req.ConfigStreamer) {
		in.ConfigStreamer = 1
	}
	if req.OvertimeMaxRound != nil {
		in.OvertimeMaxRound = *req.OvertimeMaxRound
	}
	if req.OvertimeStartmoney != nil {
		in.OvertimeStartMoney = *req.OvertimeStartmoney
	}
	in.Maps = []store.MapInput{{Name: maps[0], PickedBy: "default"}}
	in.Side = req.Side

	id, err := s.store.CreateMatch(ctx, in)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	switch {
	case req.Start:
		err = s.store.StartMatch(ctx, id, *serverID)
	case serverID != nil:
		err = s.store.AssignServer(ctx, id, *serverID)
	}
	if err != nil {
		_ = s.store.DeleteMatch(ctx, id)
		storeError(w, err, "server not found")
		return
	}

	w.Header().Set("Location", "/api/matches/"+strconv.FormatInt(id, 10))
	s.writeMatch(w, ctx, http.StatusCreated, id)
}

func (s *server) startMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	m, err := s.store.GetMatch(ctx, id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	serverID := m.ServerID
	if serverID == nil {
		free, err := s.store.FreeServers(ctx)
		if err != nil {
			storeError(w, err, "server not found")
			return
		}
		if len(free) == 0 {
			writeError(w, http.StatusConflict, "no free server available")
			return
		}
		sort.SliceStable(free, func(i, j int) bool { return free[i].Hostname < free[j].Hostname })
		serverID = &free[0].ID
	}
	if err := s.store.StartMatch(ctx, id, *serverID); err != nil {
		storeError(w, err, "match not found")
		return
	}
	s.writeMatch(w, ctx, http.StatusOK, id)
}

func (s *server) archiveMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ArchiveMatch(r.Context(), id); err != nil {
		storeError(w, err, "match not found")
		return
	}
	s.writeMatch(w, r.Context(), http.StatusOK, id)
}

func (s *server) deleteMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteMatch(r.Context(), id); err != nil {
		storeError(w, err, "match not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mapIDFilter(w http.ResponseWriter, r *http.Request) (int64, bool) {
	v := r.URL.Query().Get("map_id")
	if v == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "map_id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *server) listMatchMaps(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListMaps(r.Context(), id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	out := make([]mapJSON, 0, len(list))
	for _, m := range list {
		out = append(out, mapJSON{
			ID: m.ID, MatchID: m.MatchID, MapName: m.Name, Score1: m.Score1, Score2: m.Score2,
			CurrentSide: m.CurrentSide, Status: m.Status, MapsFor: m.PickedBy, NbOt: m.Overtimes,
			RoundsPlayed: m.Score1 + m.Score2,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) listMatchPlayers(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	mapID, ok := mapIDFilter(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListPlayers(r.Context(), id, mapID)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	out := make([]playerJSON, 0, len(list))
	for _, p := range list {
		out = append(out, playerJSON{
			ID: p.ID, MatchID: p.MatchID, MapID: p.MapID, SteamID: p.SteamID, Pseudo: p.Nickname,
			Team: p.Team, FirstSide: p.FirstSide, CurrentSide: p.CurrentSide,
			Kills: p.Kills, Assists: p.Assists, Deaths: p.Deaths, Points: p.Points,
			Headshots: p.Headshots, Defuse: p.Defuses, Bombe: p.Bombs, TeamKills: p.TeamKills,
			FirstKill: p.FirstKills,
			Nb1:       p.Rounds1K, Nb2: p.Rounds2K, Nb3: p.Rounds3K, Nb4: p.Rounds4K, Nb5: p.Rounds5K,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) listMatchRounds(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	mapID, ok := mapIDFilter(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListRoundSummaries(r.Context(), id, mapID)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	out := make([]roundJSON, 0, len(list))
	for _, rs := range list {
		out = append(out, roundJSON{
			ID: rs.ID, MatchID: rs.MatchID, MapID: rs.MapID, RoundID: rs.RoundID,
			BombPlanted: rs.BombPlanted, BombDefused: rs.BombDefused, BombExploded: rs.BombExploded,
			WinType: rs.WinType, TeamWin: rs.TeamWin, CtWin: rs.CTWin, TWin: rs.TWin,
			ScoreA: rs.ScoreA, ScoreB: rs.ScoreB, BestKiller: rs.BestKillerID, BestKillerNb: rs.BestKillerNb,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// listMatchKills is a gap: the store has no kill list yet.
func (s *server) listMatchKills(w http.ResponseWriter, r *http.Request) {
	if _, ok := pathID(w, r); !ok {
		return
	}
	if _, ok := mapIDFilter(w, r); !ok {
		return
	}
	notImplemented(w, "the kill feed")
}

func deref(p *bool) bool { return p != nil && *p }
