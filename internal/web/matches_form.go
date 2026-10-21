package web

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

const (
	matchStartDateLayout = "02.01.2006 15:04"
	matchFormLimit       = 1 << 20
	tbaMap               = "tba"
)

var (
	matchRoundChoices     = []int{15, 12, 9, 5, 3}
	matchOTRoundChoices   = []int{5, 3}
	matchAutoStartChoices = []int{5, 10, 15, 30}
	matchFlagPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,8}$`)

	matchPasswordWords = []string{
		"frosch", "gehen", "rennen", "gucken", "fliegen", "rasen", "snobb", "peter", "wackel", "dackel", "gut",
		"schlecht", "win", "loss", "tragen", "weg", "berlin", "aachen", "mensch", "tier", "turtle", "adler",
		"raupe", "rauben", "bank", "schalter", "ticket", "bahn", "zug", "delay", "flugzeug", "ratte", "nager",
		"hase", "feld", "gras", "kraut", "gurke", "apfel", "salat", "tomate", "dressing", "essig", "zwiebel",
		"kuchen", "zucker", "salz", "kaffee", "tee", "monday", "tuesday", "wednesday", "friday", "weekend",
		"holiday", "doctor", "game", "cup", "death", "player", "monitor", "hand", "food", "paper", "windows",
		"together",
	}
)

// matchModes are the series formats the form offers, as the old form did.
var matchModes = []matchChoice{
	{Value: "normal", Label: "BO1"},
	{Value: "bo3_modeb", Label: "BO3"},
}

type matchChoice struct {
	Value string
	Label string
}

// matchFormValues holds the raw form state so it can be shown again.
type matchFormValues struct {
	SeasonID      string
	TeamA         string
	TeamAName     string
	TeamAFlag     string
	TeamB         string
	TeamBName     string
	TeamBFlag     string
	Rules         string
	Password      string
	MaxRound      string
	FullScore     bool
	Streamer      bool
	KnifeRound    bool
	OT            bool
	OTMoney       string
	OTMaxRound    string
	AutoStart     bool
	StartDate     string
	AutoStartTime string
	Mode          string
	Map           string
	Side          string
	ServerID      string
}

type matchFormPage struct {
	Heading    string
	Action     string
	Submit     string
	Editing    bool
	MatchID    int64
	StatusText string
	Form       matchFormValues
	Errors     map[string]string
	Error      string

	Seasons       []store.Season
	Teams         []store.Team
	Servers       []store.Server
	MapChoices    []string
	RulesChoices  []string
	RoundChoices  []matchChoice
	OTRoundChoice []matchChoice
	AutoStartOpts []matchChoice
	Modes         []matchChoice
}

func (p matchFormPage) Title() string { return p.Heading }

type matchRefs struct {
	seasons []store.Season
	teams   []store.Team
	servers []store.Server
}

func (h *matchHandlers) refs(ctx context.Context) (matchRefs, error) {
	var r matchRefs
	var err error
	if r.seasons, err = h.st.ListSeasons(ctx); err != nil {
		return r, err
	}
	if r.teams, err = h.st.ListTeams(ctx); err != nil {
		return r, err
	}
	r.servers, err = h.st.ListServers(ctx)
	return r, err
}

func intChoices(vals []int, prefix string) []matchChoice {
	out := make([]matchChoice, len(vals))
	for i, v := range vals {
		out[i] = matchChoice{Value: strconv.Itoa(v), Label: prefix + strconv.Itoa(v)}
	}
	return out
}

// withInt returns vals plus extra when it is missing, keeping the order.
func withInt(vals []int, extra int) []int {
	if extra > 0 && !slices.Contains(vals, extra) {
		return append(slices.Clone(vals), extra)
	}
	return vals
}

func (h *matchHandlers) defaultsForm() matchFormValues {
	d := h.app.Cfg.Defaults
	maxRound := d.MaxRound
	if !slices.Contains(matchRoundChoices, maxRound) {
		maxRound = 12
	}
	pw := d.Password
	if pw == "" {
		pw = matchPasswordWords[rand.IntN(len(matchPasswordWords))]
	}
	return matchFormValues{
		Rules:         d.Rules,
		Password:      pw,
		MaxRound:      strconv.Itoa(maxRound),
		KnifeRound:    true,
		OT:            d.OvertimeEnable,
		OTMoney:       strconv.Itoa(d.OvertimeStartMoney),
		OTMaxRound:    strconv.Itoa(d.OvertimeMaxRound),
		StartDate:     time.Now().Format(matchStartDateLayout),
		AutoStartTime: "5",
		Mode:          "normal",
	}
}

func formFromMatch(m store.Match, maps []store.Map) matchFormValues {
	f := matchFormValues{
		Rules:         m.Rules,
		Password:      m.ConfigPassword,
		MaxRound:      strconv.Itoa(m.MaxRound),
		FullScore:     m.ConfigFullScore,
		Streamer:      m.ConfigStreamer > 0,
		KnifeRound:    m.ConfigKnifeRound,
		OT:            m.ConfigOT,
		OTMoney:       strconv.Itoa(m.OvertimeStartMoney),
		OTMaxRound:    strconv.Itoa(m.OvertimeMaxRound),
		AutoStart:     m.AutoStart,
		AutoStartTime: strconv.Itoa(m.AutoStartTime),
		Mode:          m.MapSelectionMode,
		TeamAName:     m.TeamAName,
		TeamAFlag:     m.TeamAFlag,
		TeamBName:     m.TeamBName,
		TeamBFlag:     m.TeamBFlag,
	}
	if f.Mode == "" {
		f.Mode = "normal"
	}
	if m.SeasonID != nil {
		f.SeasonID = strconv.FormatInt(*m.SeasonID, 10)
	}
	if m.TeamAID != nil {
		f.TeamA = strconv.FormatInt(*m.TeamAID, 10)
	}
	if m.TeamBID != nil {
		f.TeamB = strconv.FormatInt(*m.TeamBID, 10)
	}
	if m.ServerID != nil {
		f.ServerID = strconv.FormatInt(*m.ServerID, 10)
	}
	if m.StartDate != nil {
		f.StartDate = m.StartDate.Local().Format(matchStartDateLayout)
	} else {
		f.StartDate = time.Now().Format(matchStartDateLayout)
	}
	for _, mp := range maps {
		if m.CurrentMapID != nil && mp.ID == *m.CurrentMapID {
			f.Map = mp.Name
			f.Side = mp.CurrentSide
		}
	}
	if f.Map == "" && len(maps) > 0 {
		f.Map = maps[0].Name
		f.Side = maps[0].CurrentSide
	}
	return f
}

func (h *matchHandlers) formPage(refs matchRefs, f matchFormValues, existing *store.Match) matchFormPage {
	cfg := h.app.Cfg
	p := matchFormPage{
		Heading:       "Create new match",
		Action:        "/admin/matches",
		Submit:        "Create match",
		Form:          f,
		Seasons:       refs.seasons,
		Teams:         refs.teams,
		Servers:       refs.servers,
		MapChoices:    mapChoices(cfg.Maps, existing),
		RulesChoices:  cfg.Rules,
		AutoStartOpts: intChoices(matchAutoStartChoices, ""),
		Modes:         matchModes,
	}
	for i := range p.AutoStartOpts {
		p.AutoStartOpts[i].Label = fmt.Sprintf("%02d minutes before the start date", matchAutoStartChoices[i])
	}
	rounds := matchRoundChoices
	otRounds := withInt(matchOTRoundChoices, cfg.Defaults.OvertimeMaxRound)
	if existing != nil {
		rounds = withInt(rounds, existing.MaxRound)
		otRounds = withInt(otRounds, existing.OvertimeMaxRound)
		if existing.Rules != "" && !slices.Contains(p.RulesChoices, existing.Rules) {
			p.RulesChoices = append(slices.Clone(p.RulesChoices), existing.Rules)
		}
		if m := existing.MapSelectionMode; m != "" && !slices.ContainsFunc(p.Modes, func(c matchChoice) bool { return c.Value == m }) {
			p.Modes = append(slices.Clone(p.Modes), matchChoice{Value: m, Label: m})
		}
		p.Heading = fmt.Sprintf("Edit match #%d: %s vs %s", existing.ID, existing.TeamAName, existing.TeamBName)
		p.Action = fmt.Sprintf("/admin/matches/%d", existing.ID)
		p.Submit = "Save match"
		p.Editing = true
		p.MatchID = existing.ID
		p.StatusText = store.StatusLabel(existing.Status)
	}
	p.RoundChoices = intChoices(rounds, "MR")
	p.OTRoundChoice = intChoices(otRounds, "MR")
	return p
}

func (h *matchHandlers) newForm(w http.ResponseWriter, r *http.Request) {
	refs, err := h.refs(r.Context())
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	f := h.defaultsForm()
	f.Map = firstOr(h.app.Cfg.Maps, tbaMap)
	h.app.Render(w, r, "match_form", h.formPage(refs, f, nil))
}

// mapChoices is the map select: the configured maps, "tba" (pick in game by
// veto) and "CurrentMap" (stay on the server's map, for workshop maps). On
// edit the stored name is kept as a choice, because the bot renames
// "CurrentMap" to "CurrentMap-<map>" and a map may have left EBOT_MAPS.
// rulesName is the file name the bot runs as "exec <rules>.cfg" over RCON, so
// it must never contain a separator or a space.
var rulesName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)

func mapChoices(maps []string, existing *store.Match) []string {
	out := append(slices.Clone(maps), tbaMap, config.CurrentMap)
	if existing != nil && existing.CurrentMap != nil && !slices.Contains(out, existing.CurrentMap.Name) {
		out = append(out, existing.CurrentMap.Name)
	}
	return out
}

func init() {
	registerFunc("mapLabel", func(name string) string {
		switch {
		case name == tbaMap:
			return "Choose by map veto"
		case name == config.CurrentMap:
			return "Current server map (no map change)"
		case config.IsCurrentMap(name):
			if _, real, ok := strings.Cut(name, "-"); ok && real != "" {
				return real + " (server map)"
			}
		}
		return name
	})
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

func (h *matchHandlers) editForm(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	ctx := r.Context()
	m, err := h.st.GetMatch(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	if m.Enable {
		h.app.SetFlash(w, "err", "Match is currently in progress, it cannot be edited.")
		Redirect(w, r, fmt.Sprintf("/matches/%d", id))
		return
	}
	maps, err := h.st.ListMaps(ctx, id)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	refs, err := h.refs(ctx)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	h.app.Render(w, r, "match_form", h.formPage(refs, formFromMatch(*m, maps), m))
}

func formBool(r *http.Request, name string) bool { return r.PostForm.Get(name) != "" }

func intIn(s string, allowed []int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || !slices.Contains(allowed, n) {
		return 0, false
	}
	return n, true
}

// parseMatchForm reads and validates the posted form. existing is the match
// being edited, or nil when creating. The returned map holds one message per
// field name.
func (h *matchHandlers) parseMatchForm(r *http.Request, refs matchRefs, existing *store.Match) (matchFormValues, store.MatchInput, map[string]string) {
	cfg := h.app.Cfg
	pf := r.PostForm
	errs := map[string]string{}
	var in store.MatchInput

	f := matchFormValues{
		SeasonID:      strings.TrimSpace(pf.Get("season_id")),
		TeamA:         strings.TrimSpace(pf.Get("team_a")),
		TeamAName:     strings.TrimSpace(pf.Get("team_a_name")),
		TeamAFlag:     strings.TrimSpace(pf.Get("team_a_flag")),
		TeamB:         strings.TrimSpace(pf.Get("team_b")),
		TeamBName:     strings.TrimSpace(pf.Get("team_b_name")),
		TeamBFlag:     strings.TrimSpace(pf.Get("team_b_flag")),
		Rules:         strings.TrimSpace(pf.Get("rules")),
		Password:      strings.TrimSpace(pf.Get("config_password")),
		MaxRound:      strings.TrimSpace(pf.Get("max_round")),
		FullScore:     formBool(r, "config_full_score"),
		Streamer:      formBool(r, "config_streamer"),
		KnifeRound:    formBool(r, "config_knife_round"),
		OT:            formBool(r, "config_ot"),
		OTMoney:       strings.TrimSpace(pf.Get("overtime_startmoney")),
		OTMaxRound:    strings.TrimSpace(pf.Get("overtime_max_round")),
		AutoStart:     formBool(r, "auto_start"),
		StartDate:     strings.TrimSpace(pf.Get("startdate")),
		AutoStartTime: strings.TrimSpace(pf.Get("auto_start_time")),
		Mode:          strings.TrimSpace(pf.Get("map_selection_mode")),
		ServerID:      strings.TrimSpace(pf.Get("server_id")),
	}
	f.Map = strings.TrimSpace(pf.Get("map"))
	f.Side = strings.TrimSpace(pf.Get("side"))

	// Season.
	if f.SeasonID != "" && f.SeasonID != "0" {
		id, err := strconv.ParseInt(f.SeasonID, 10, 64)
		if err != nil || !slices.ContainsFunc(refs.seasons, func(s store.Season) bool { return s.ID == id }) {
			errs["season_id"] = "Choose a season from the list."
		} else {
			in.SeasonID = &id
		}
	}

	// Teams.
	teamByID := func(raw string) (*store.Team, bool) {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, false
		}
		for i := range refs.teams {
			if refs.teams[i].ID == id {
				return &refs.teams[i], true
			}
		}
		return nil, false
	}
	side := func(key, raw, name, flag string) (id *int64, outName, outFlag string) {
		if raw != "" && raw != "0" {
			t, ok := teamByID(raw)
			if !ok {
				errs[key] = "Choose a team from the list."
				return nil, "", ""
			}
			return &t.ID, t.Name, t.Flag
		}
		if name == "" {
			errs[key+"_name"] = "Choose a team or enter a team name."
		} else if utf8.RuneCountInString(name) > 255 {
			errs[key+"_name"] = "At most 255 characters."
		}
		if flag != "" && !matchFlagPattern.MatchString(flag) {
			errs[key+"_flag"] = "Use a country code such as DK."
		}
		return nil, name, strings.ToUpper(flag)
	}
	in.TeamAID, in.TeamAName, in.TeamAFlag = side("team_a", f.TeamA, f.TeamAName, f.TeamAFlag)
	in.TeamBID, in.TeamBName, in.TeamBFlag = side("team_b", f.TeamB, f.TeamBName, f.TeamBFlag)
	if in.TeamAID != nil && in.TeamBID != nil && *in.TeamAID == *in.TeamBID {
		errs["team_b"] = "Team A and team B must differ."
	}

	// Rules.
	switch {
	case f.Rules == "":
		errs["rules"] = "Required."
	case !rulesName.MatchString(f.Rules):
		errs["rules"] = "Use letters, digits, dot, dash and underscore only."
	default:
		in.Rules = f.Rules
	}

	if utf8.RuneCountInString(f.Password) > 100 {
		errs["config_password"] = "At most 100 characters."
	}
	in.ConfigPassword = f.Password

	// Rounds.
	rounds := matchRoundChoices
	otRounds := withInt(matchOTRoundChoices, cfg.Defaults.OvertimeMaxRound)
	if existing != nil {
		rounds = withInt(rounds, existing.MaxRound)
		otRounds = withInt(otRounds, existing.OvertimeMaxRound)
	}
	if n, ok := intIn(f.MaxRound, rounds); ok {
		in.MaxRound = n
	} else {
		errs["max_round"] = "Choose the number of rounds from the list."
	}

	in.ConfigFullScore = f.FullScore
	in.ConfigKnifeRound = f.KnifeRound
	in.ConfigOT = f.OT
	if existing != nil {
		in.ConfigStreamer = existing.ConfigStreamer
		in.ConfigSwitchAuto = existing.ConfigSwitchAuto
		in.ConfigAutoChangePassword = existing.ConfigAutoChangePassword
		in.ConfigHeatmap = existing.ConfigHeatmap
		in.IngameEnable = existing.IngameEnable
		in.ForceZoomMatch = existing.ForceZoomMatch
		in.IdentifierID = existing.IdentifierID
	}
	switch {
	case !f.Streamer:
		in.ConfigStreamer = 0
	case in.ConfigStreamer == 0:
		in.ConfigStreamer = 1
	}

	// Overtime.
	in.OvertimeStartMoney = cfg.Defaults.OvertimeStartMoney
	in.OvertimeMaxRound = cfg.Defaults.OvertimeMaxRound
	if f.OT || f.OTMoney != "" {
		n, err := strconv.Atoi(f.OTMoney)
		if err != nil || n < 0 || n > 1_000_000 {
			errs["overtime_startmoney"] = "Enter an amount between 0 and 1000000."
		} else {
			in.OvertimeStartMoney = n
		}
	}
	if f.OT || f.OTMaxRound != "" {
		if n, ok := intIn(f.OTMaxRound, otRounds); ok {
			in.OvertimeMaxRound = n
		} else {
			errs["overtime_max_round"] = "Choose the number of rounds from the list."
		}
	}

	// Autostart. Without a start date, autostart is switched off, as before.
	in.AutoStartTime = 5
	if f.StartDate != "" {
		t, err := time.ParseInLocation(matchStartDateLayout, f.StartDate, time.Local)
		if err != nil {
			errs["startdate"] = "Use the format dd.mm.yyyy hh:mm."
		} else {
			in.StartDate = &t
		}
	}
	if f.AutoStart {
		if n, ok := intIn(f.AutoStartTime, matchAutoStartChoices); ok {
			in.AutoStartTime = n
		} else {
			errs["auto_start_time"] = "Choose a time from the list."
		}
		in.AutoStart = in.StartDate != nil
	}

	// Maps.
	modes := make([]string, 0, len(matchModes)+1)
	for _, m := range matchModes {
		modes = append(modes, m.Value)
	}
	if existing != nil && existing.MapSelectionMode != "" {
		modes = append(modes, existing.MapSelectionMode)
	}
	if f.Mode == "" {
		f.Mode = "normal"
	}
	if !slices.Contains(modes, f.Mode) {
		errs["map_selection_mode"] = "Choose BO1 or BO3."
	} else {
		in.MapSelectionMode = f.Mode
	}
	valid := mapChoices(cfg.Maps, existing)
	switch {
	case f.Map == "":
		errs["map"] = "Required."
	case !slices.Contains(valid, f.Map):
		errs["map"] = "Choose a map from the list."
	default:
		in.Maps = []store.MapInput{{Name: f.Map, PickedBy: "default"}}
	}
	switch f.Side {
	case "", "ct", "t":
		in.Side = f.Side
	default:
		errs["side"] = "Choose a side from the list."
	}

	// Server.
	if f.ServerID != "" && f.ServerID != "0" {
		id, err := strconv.ParseInt(f.ServerID, 10, 64)
		if err != nil || !slices.ContainsFunc(refs.servers, func(s store.Server) bool { return s.ID == id }) {
			errs["server_id"] = "Choose a server from the list."
		} else {
			in.ServerID = &id
		}
	}

	return f, in, errs
}

func (h *matchHandlers) readPost(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, matchFormLimit)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func (h *matchHandlers) rerender(w http.ResponseWriter, r *http.Request, refs matchRefs, f matchFormValues, existing *store.Match, errs map[string]string) {
	p := h.formPage(refs, f, existing)
	p.Errors = errs
	p.Error = "Please correct the marked fields."
	h.app.RenderStatus(w, r, http.StatusUnprocessableEntity, "match_form", p)
}

func (h *matchHandlers) create(w http.ResponseWriter, r *http.Request) {
	if !h.readPost(w, r) {
		return
	}
	refs, err := h.refs(r.Context())
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	f, in, errs := h.parseMatchForm(r, refs, nil)
	if len(errs) > 0 {
		h.rerender(w, r, refs, f, nil, errs)
		return
	}
	id, err := h.st.CreateMatch(r.Context(), in)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	h.app.SetFlash(w, "ok", fmt.Sprintf("Match created with ID %d.", id))
	Redirect(w, r, "/admin/matches/new")
}

func (h *matchHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	if !h.readPost(w, r) {
		return
	}
	ctx := r.Context()
	m, err := h.st.GetMatch(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	if m.Enable {
		h.matchEnabled(w, r, id)
		return
	}
	refs, err := h.refs(ctx)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	f, in, errs := h.parseMatchForm(r, refs, m)
	if len(errs) > 0 {
		h.rerender(w, r, refs, f, m, errs)
		return
	}
	err = h.st.UpdateMatch(ctx, id, in)
	switch {
	case errors.Is(err, store.ErrMatchEnabled):
		h.matchEnabled(w, r, id)
	case errors.Is(err, store.ErrNotFound):
		h.app.NotFound(w, r)
	case err != nil:
		h.app.ServerError(w, r, err)
	default:
		h.app.SetFlash(w, "ok", "Match edited successfully.")
		Redirect(w, r, fmt.Sprintf("/matches/%d", id))
	}
}

func (h *matchHandlers) matchEnabled(w http.ResponseWriter, r *http.Request, id int64) {
	h.app.SetFlash(w, "err", "Match is currently in progress, it cannot be edited.")
	Redirect(w, r, fmt.Sprintf("/matches/%d", id))
}
