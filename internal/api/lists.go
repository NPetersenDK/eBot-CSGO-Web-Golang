package api

import (
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

func toServer(sv *store.Server) gameServer {
	return gameServer{ID: sv.ID, IP: sv.IP, Rcon: sv.RCON, Hostname: sv.Hostname, TvIP: sv.TVIP}
}

func (s *server) listServers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListServers(r.Context())
	if err != nil {
		storeError(w, err, "server not found")
		return
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Hostname < list[j].Hostname })
	out := make([]gameServer, 0, len(list))
	for i := range list {
		gs := toServer(&list[i])
		gs.Rcon = ""
		out = append(out, gs)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getServer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sv, err := s.store.GetServer(r.Context(), id)
	if err != nil {
		storeError(w, err, "server not found")
		return
	}
	writeJSON(w, http.StatusOK, toServer(sv))
}

func decodeServer(w http.ResponseWriter, r *http.Request) (serverRequest, bool) {
	var req serverRequest
	if !decodeJSON(w, r, &req) {
		return req, false
	}
	req.IP = strings.TrimSpace(req.IP)
	req.Rcon = strings.TrimSpace(req.Rcon)
	req.Hostname = strings.TrimSpace(req.Hostname)
	if req.IP == "" || req.Rcon == "" || req.Hostname == "" {
		writeError(w, http.StatusBadRequest, "ip, rcon and hostname are required")
		return req, false
	}
	return req, true
}

func (s *server) createServer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeServer(w, r)
	if !ok {
		return
	}
	for _, err := range []error{store.CheckServerAddress(req.IP), store.CheckServerText("hostname", req.Hostname), store.CheckServerText("tv_ip", req.TvIP)} {
		if err != nil {
			storeError(w, err, "")
			return
		}
	}
	host, portStr, err := net.SplitHostPort(req.IP)
	port, perr := strconv.Atoi(portStr)
	if err != nil || perr != nil || port < 1 || port > 65535 {
		writeError(w, http.StatusBadRequest, "ip must be host:port")
		return
	}
	mode := "hostname"
	if net.ParseIP(host) != nil {
		mode = "single"
	}
	ctx := r.Context()
	if _, err := s.store.CreateServers(ctx, store.ServerCreate{
		Mode: mode, Host: host, Port: port, RCON: req.Rcon, Hostname: req.Hostname, TVIP: req.TvIP,
	}); err != nil {
		storeError(w, err, "server not found")
		return
	}
	list, err := s.store.ListServers(ctx)
	if err != nil {
		storeError(w, err, "server not found")
		return
	}
	var created *store.Server
	for i := range list {
		if list[i].IP == req.IP && (created == nil || list[i].ID > created.ID) {
			created = &list[i]
		}
	}
	if created == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, toServer(created))
}

func (s *server) updateServer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, ok := decodeServer(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := s.store.UpdateServer(ctx, id, store.ServerInput{
		IP: req.IP, RCON: req.Rcon, Hostname: req.Hostname, TVIP: req.TvIP,
	}); err != nil {
		storeError(w, err, "server not found")
		return
	}
	sv, err := s.store.GetServer(ctx, id)
	if err != nil {
		storeError(w, err, "server not found")
		return
	}
	writeJSON(w, http.StatusOK, toServer(sv))
}

func (s *server) deleteServer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteServer(r.Context(), id); err != nil {
		storeError(w, err, "server not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listTeams(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTeams(r.Context())
	if err != nil {
		storeError(w, err, "team not found")
		return
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	out := make([]teamJSON, 0, len(list))
	for _, t := range list {
		out = append(out, teamJSON{ID: t.ID, Name: t.Name, Shorthandle: t.ShortName, Flag: t.Flag, Link: t.Link})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) listSeasons(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSeasons(r.Context())
	if err != nil {
		storeError(w, err, "season not found")
		return
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	out := make([]seasonJSON, 0, len(list))
	for _, se := range list {
		out = append(out, seasonJSON{
			ID: se.ID, Name: se.Name, Event: se.Event, Start: parseDate(se.Start), End: parseDate(se.End),
			Link: se.Link, Logo: se.Logo, Active: se.Active,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func parseDate(v string) *time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}
