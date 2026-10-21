package api

import (
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

type matchJSON struct {
	ID               int64      `json:"id"`
	SeasonID         *int64     `json:"season_id"`
	TeamA            *int64     `json:"team_a"`
	TeamAName        string     `json:"team_a_name"`
	TeamAFlag        string     `json:"team_a_flag"`
	TeamB            *int64     `json:"team_b"`
	TeamBName        string     `json:"team_b_name"`
	TeamBFlag        string     `json:"team_b_flag"`
	Status           int        `json:"status"`
	StatusText       string     `json:"status_text"`
	IsPaused         bool       `json:"is_paused"`
	ScoreA           int        `json:"score_a"`
	ScoreB           int        `json:"score_b"`
	MaxRound         int        `json:"max_round"`
	Rules            string     `json:"rules"`
	MapSelectionMode string     `json:"map_selection_mode"`
	ConfigPassword   string     `json:"config_password"`
	Enable           bool       `json:"enable"`
	ServerID         *int64     `json:"server_id"`
	IP               string     `json:"ip"`
	CurrentMap       *int64     `json:"current_map"`
	Startdate        *time.Time `json:"startdate"`
	CreatedAt        *time.Time `json:"created_at"`
}

func toMatch(m *store.Match) matchJSON {
	out := matchJSON{
		ID: m.ID, SeasonID: m.SeasonID,
		TeamA: m.TeamAID, TeamAName: m.TeamAName, TeamAFlag: m.TeamAFlag,
		TeamB: m.TeamBID, TeamBName: m.TeamBName, TeamBFlag: m.TeamBFlag,
		Status: m.Status, StatusText: statusText(m.Status), IsPaused: m.IsPaused,
		ScoreA: m.ScoreA, ScoreB: m.ScoreB, MaxRound: m.MaxRound, Rules: m.Rules,
		MapSelectionMode: m.MapSelectionMode, ConfigPassword: m.ConfigPassword,
		Enable: m.Enable, ServerID: m.ServerID, IP: m.IP, CurrentMap: m.CurrentMapID,
		Startdate: m.StartDate,
	}
	if !m.CreatedAt.IsZero() {
		t := m.CreatedAt
		out.CreatedAt = &t
	}
	return out
}

type createMatchRequest struct {
	TeamAID *int64 `json:"team_a_id"`
	TeamBID *int64 `json:"team_b_id"`

	TeamAName string `json:"team_a_name"`
	TeamBName string `json:"team_b_name"`
	TeamAFlag string `json:"team_a_flag"`
	TeamBFlag string `json:"team_b_flag"`

	SeasonID *int64 `json:"season_id"`

	MaxRound         int    `json:"max_round"`
	Rules            string `json:"rules"`
	MapSelectionMode string `json:"map_selection_mode"`
	ConfigPassword   string `json:"config_password"`

	ConfigKnifeRound   *bool `json:"config_knife_round"`
	ConfigOt           *bool `json:"config_ot"`
	ConfigFullScore    *bool `json:"config_full_score"`
	ConfigStreamer     *bool `json:"config_streamer"`
	ConfigSwitchAuto   *bool `json:"config_switch_auto"`
	OvertimeMaxRound   *int  `json:"overtime_max_round"`
	OvertimeStartmoney *int  `json:"overtime_startmoney"`

	Maps []string `json:"maps"`
	// Side is accepted for compatibility. The store has no per-map side input.
	Side string `json:"side"`

	ServerID *int64 `json:"server_id"`
	Start    bool   `json:"start"`
}

type gameServer struct {
	ID       int64  `json:"id"`
	IP       string `json:"ip"`
	Rcon     string `json:"rcon"`
	Hostname string `json:"hostname"`
	TvIP     string `json:"tv_ip"`
}

type serverRequest struct {
	IP       string `json:"ip"`
	Rcon     string `json:"rcon"`
	Hostname string `json:"hostname"`
	TvIP     string `json:"tv_ip"`
}

type teamJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Shorthandle string `json:"shorthandle"`
	Flag        string `json:"flag"`
	Link        string `json:"link"`
}

type seasonJSON struct {
	ID     int64      `json:"id"`
	Name   string     `json:"name"`
	Event  string     `json:"event"`
	Start  *time.Time `json:"start"`
	End    *time.Time `json:"end"`
	Link   string     `json:"link"`
	Logo   string     `json:"logo"`
	Active bool       `json:"active"`
}

type mapJSON struct {
	ID           int64  `json:"id"`
	MatchID      int64  `json:"match_id"`
	MapName      string `json:"map_name"`
	Score1       int    `json:"score_1"`
	Score2       int    `json:"score_2"`
	CurrentSide  string `json:"current_side"`
	Status       int    `json:"status"`
	MapsFor      string `json:"maps_for"`
	NbOt         int    `json:"nb_ot"`
	RoundsPlayed int    `json:"rounds_played"`
}

type playerJSON struct {
	ID          int64  `json:"id"`
	MatchID     int64  `json:"match_id"`
	MapID       int64  `json:"map_id"`
	SteamID     string `json:"steamid"`
	Pseudo      string `json:"pseudo"`
	Team        string `json:"team"`
	FirstSide   string `json:"first_side"`
	CurrentSide string `json:"current_side"`
	Kills       int    `json:"kills"`
	Assists     int    `json:"assists"`
	Deaths      int    `json:"deaths"`
	Points      int    `json:"points"`
	Headshots   int    `json:"hs"`
	Defuse      int    `json:"defuse"`
	Bombe       int    `json:"bombe"`
	TeamKills   int    `json:"tk"`
	FirstKill   int    `json:"firstkill"`
	Nb1         int    `json:"nb1"`
	Nb2         int    `json:"nb2"`
	Nb3         int    `json:"nb3"`
	Nb4         int    `json:"nb4"`
	Nb5         int    `json:"nb5"`
}

type roundJSON struct {
	ID           int64  `json:"id"`
	MatchID      int64  `json:"match_id"`
	MapID        int64  `json:"map_id"`
	RoundID      int    `json:"round_id"`
	BombPlanted  bool   `json:"bomb_planted"`
	BombDefused  bool   `json:"bomb_defused"`
	BombExploded bool   `json:"bomb_exploded"`
	WinType      string `json:"win_type"`
	TeamWin      string `json:"team_win"`
	CtWin        bool   `json:"ct_win"`
	TWin         bool   `json:"t_win"`
	ScoreA       int    `json:"score_a"`
	ScoreB       int    `json:"score_b"`
	BestKiller   *int64 `json:"best_killer"`
	BestKillerNb int    `json:"best_killer_nb"`
}

func statusText(s int) string {
	texts := [...]string{
		"Not started", "Starting", "Warmup knife", "Knife", "End knife",
		"Warmup 1st side", "First side", "Warmup 2nd side", "Second side",
		"Warmup OT 1st side", "OT first side", "Warmup OT 2nd side", "OT second side",
		"End match", "Archived",
	}
	if s < 0 || s >= len(texts) {
		return "Unknown"
	}
	return texts[s]
}
