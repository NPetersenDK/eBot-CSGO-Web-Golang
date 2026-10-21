package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

// loadMatch fetches the match named by the path. It writes the error page and
// returns false when the match cannot be used.
func (h *matchHandlers) loadMatch(w http.ResponseWriter, r *http.Request) (*store.Match, bool) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return nil, false
	}
	m, err := h.st.GetMatch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return nil, false
	}
	return m, true
}

// finish turns the store result of an action into a flash and a redirect.
func (h *matchHandlers) finish(w http.ResponseWriter, r *http.Request, err error, okMsg, to string) {
	switch {
	case err == nil:
		h.app.SetFlash(w, "ok", okMsg)
	case errors.Is(err, store.ErrNotFound):
		h.app.NotFound(w, r)
		return
	case errors.Is(err, store.ErrMatchEnabled):
		h.app.SetFlash(w, "err", "The match is running. Stop it first.")
	case errors.Is(err, store.ErrServerBusy):
		h.app.SetFlash(w, "err", "That server is in use by another match.")
	default:
		h.app.ServerError(w, r, err)
		return
	}
	Redirect(w, r, to)
}

func matchURL(id int64) string { return fmt.Sprintf("/matches/%d", id) }

// postedServer reads form field server_id. found is false when it is empty.
func (h *matchHandlers) postedServer(w http.ResponseWriter, r *http.Request) (srv *store.Server, found, ok bool) {
	raw := strings.TrimSpace(r.PostFormValue("server_id"))
	if raw == "" || raw == "0" {
		return nil, false, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		h.app.NotFound(w, r)
		return nil, false, false
	}
	s, err := h.st.GetServer(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return nil, false, false
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return nil, false, false
	}
	return s, true, true
}

func (h *matchHandlers) start(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMatch(w, r)
	if !ok {
		return
	}
	if !h.readPost(w, r) {
		return
	}
	if m.Enable {
		h.app.SetFlash(w, "err", "The match is already running.")
		Redirect(w, r, matchURL(m.ID))
		return
	}
	ctx := r.Context()
	srv, found, ok := h.postedServer(w, r)
	if !ok {
		return
	}
	if !found && m.ServerID != nil {
		srv, err := h.st.GetServer(ctx, *m.ServerID)
		if err == nil {
			h.startOn(w, r, m, srv)
			return
		}
		if !errors.Is(err, store.ErrNotFound) {
			h.app.ServerError(w, r, err)
			return
		}
	}
	if !found {
		free, err := h.st.FreeServers(ctx)
		if err != nil {
			h.app.ServerError(w, r, err)
			return
		}
		if len(free) == 0 {
			h.app.SetFlash(w, "err", "No server available.")
			Redirect(w, r, matchURL(m.ID))
			return
		}
		pick := free[rand.IntN(len(free))]
		srv = &pick
	}
	h.startOn(w, r, m, srv)
}

func (h *matchHandlers) startOn(w http.ResponseWriter, r *http.Request, m *store.Match, srv *store.Server) {
	err := h.st.StartMatch(r.Context(), m.ID, srv.ID)
	h.finish(w, r, err, "Match will be started on "+srv.IP+".", matchURL(m.ID))
}

func (h *matchHandlers) assignServer(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMatch(w, r)
	if !ok {
		return
	}
	if !h.readPost(w, r) {
		return
	}
	srv, found, ok := h.postedServer(w, r)
	if !ok {
		return
	}
	if !found {
		h.app.SetFlash(w, "err", "Choose a server.")
		Redirect(w, r, matchURL(m.ID))
		return
	}
	if m.Status != store.StatusNotStarted {
		h.app.SetFlash(w, "err", "A server can only be assigned before the match starts.")
		Redirect(w, r, matchURL(m.ID))
		return
	}
	err := h.st.AssignServer(r.Context(), m.ID, srv.ID)
	h.finish(w, r, err, "Server assigned.", matchURL(m.ID))
}

func (h *matchHandlers) simple(do func(ctx context.Context, id int64) error, okMsg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := matchID(r)
		if !ok {
			h.app.NotFound(w, r)
			return
		}
		h.finish(w, r, do(r.Context(), id), okMsg, matchURL(id))
	}
}

func (h *matchHandlers) retry(w http.ResponseWriter, r *http.Request) {
	h.simple(h.st.RetryMatch, "Match is restarted.")(w, r)
}

func (h *matchHandlers) reset(w http.ResponseWriter, r *http.Request) {
	h.simple(h.st.ResetMatch, "Match is reset.")(w, r)
}

func (h *matchHandlers) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	h.finish(w, r, h.st.ArchiveMatch(r.Context(), id), "Match archived.", "/matches")
}

func (h *matchHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	h.finish(w, r, h.st.DeleteMatch(r.Context(), id), "Match deleted.", "/matches")
}

func (h *matchHandlers) duplicate(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		h.app.NotFound(w, r)
		return
	}
	newID, err := h.st.DuplicateMatch(r.Context(), id)
	if err != nil {
		h.finish(w, r, err, "", "")
		return
	}
	h.finish(w, r, nil, fmt.Sprintf("Match cloned with ID %d.", newID), matchURL(newID))
}

type matchScoreMap struct {
	Map    store.Map
	Scores []store.MapScore
}

type matchScorePage struct {
	Match store.Match
	Maps  []matchScoreMap
}

func (p matchScorePage) Title() string {
	return fmt.Sprintf("Scores of match #%d", p.Match.ID)
}

func (h *matchHandlers) scores(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMatch(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	maps, err := h.st.ListMaps(ctx, m.ID)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	page := matchScorePage{Match: *m}
	for _, mp := range maps {
		sc, err := h.st.ListMapScores(ctx, mp.ID)
		if err != nil {
			h.app.ServerError(w, r, err)
			return
		}
		page.Maps = append(page.Maps, matchScoreMap{Map: mp, Scores: sc})
	}
	h.app.Render(w, r, "match_score", page)
}

func scoreField(r *http.Request, name string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue(name)))
	return n, err == nil && n >= 0 && n <= 99
}

func (h *matchHandlers) updateScore(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMatch(w, r)
	if !ok {
		return
	}
	if !h.readPost(w, r) {
		return
	}
	back := fmt.Sprintf("/admin/matches/%d/scores", m.ID)
	scoreID, err := strconv.ParseInt(r.PathValue("scoreId"), 10, 64)
	if err != nil || scoreID < 1 {
		h.app.NotFound(w, r)
		return
	}
	ctx := r.Context()
	sc, err := h.st.GetMapScore(ctx, scoreID)
	if errors.Is(err, store.ErrNotFound) {
		h.app.NotFound(w, r)
		return
	}
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	maps, err := h.st.ListMaps(ctx, m.ID)
	if err != nil {
		h.app.ServerError(w, r, err)
		return
	}
	owned := false
	for _, mp := range maps {
		if mp.ID == sc.MapID {
			owned = true
		}
	}
	if !owned {
		h.app.NotFound(w, r)
		return
	}
	if m.Enable {
		h.app.SetFlash(w, "err", "Match is currently in progress.")
		Redirect(w, r, back)
		return
	}
	var in store.MapScoreInput
	var good [4]bool
	in.Score1Side1, good[0] = scoreField(r, "score1_side1")
	in.Score1Side2, good[1] = scoreField(r, "score1_side2")
	in.Score2Side1, good[2] = scoreField(r, "score2_side1")
	in.Score2Side2, good[3] = scoreField(r, "score2_side2")
	if !(good[0] && good[1] && good[2] && good[3]) {
		h.app.SetFlash(w, "err", "Invalid data. Scores are whole numbers from 0 to 99.")
		Redirect(w, r, back)
		return
	}
	h.finish(w, r, h.st.UpdateMapScore(ctx, scoreID, in), "Score updated successfully.", back)
}

type commandReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeCommandReply(w http.ResponseWriter, status int, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(commandReply{OK: errMsg == "", Error: errMsg})
}

// checkCommandArg validates the argument for the actions that take one.
func checkCommandArg(action, arg string) string {
	switch action {
	case "executeCommand":
		switch {
		case arg == "":
			return "The command is empty."
		case utf8.RuneCountInString(arg) > 500:
			return "The command is too long."
		case strings.ContainsAny(arg, "\r\n\x00"):
			return "The command must be a single line."
		}
	case "goBackRounds":
		if n, err := strconv.Atoi(arg); err != nil || n < 0 || n > 999 {
			return "The round must be a number."
		}
	}
	return ""
}

func (h *matchHandlers) command(w http.ResponseWriter, r *http.Request) {
	id, ok := matchID(r)
	if !ok {
		writeCommandReply(w, http.StatusNotFound, "Match not found.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeCommandReply(w, http.StatusBadRequest, "Bad request.")
		return
	}
	action := strings.TrimSpace(r.PostForm.Get("action"))
	if !ebotcmd.ValidAction(action) {
		writeCommandReply(w, http.StatusBadRequest, "Unknown action.")
		return
	}
	arg := strings.TrimSpace(r.PostForm.Get("arg"))
	if action != "executeCommand" && action != "goBackRounds" {
		arg = ""
	}
	if msg := checkCommandArg(action, arg); msg != "" {
		writeCommandReply(w, http.StatusBadRequest, msg)
		return
	}
	m, err := h.st.GetMatch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeCommandReply(w, http.StatusNotFound, "Match not found.")
		return
	}
	if err != nil {
		h.app.Log.Error("command: load match", "match", id, "err", err)
		writeCommandReply(w, http.StatusBadGateway, "Could not load the match.")
		return
	}
	if m.IP == "" || m.ConfigAuthKey == "" {
		writeCommandReply(w, http.StatusBadRequest, "The match has no server or auth key yet.")
		return
	}
	if !controllable(m.IP) {
		writeCommandReply(w, http.StatusBadRequest, "The bot cannot control a server added by hostname. Add it by ip:port.")
		return
	}
	if h.cmd == nil {
		writeCommandReply(w, http.StatusBadGateway, "The command channel is not configured.")
		return
	}
	if err := h.cmd.Send(r.Context(), id, action, arg, m.IP, m.ConfigAuthKey); err != nil {
		h.app.Log.Error("command: send", "match", id, "action", action, "err", err)
		writeCommandReply(w, http.StatusBadGateway, "Could not send the command.")
		return
	}
	h.app.Log.Info("command sent", "match", id, "action", action)
	writeCommandReply(w, http.StatusOK, "")
}
