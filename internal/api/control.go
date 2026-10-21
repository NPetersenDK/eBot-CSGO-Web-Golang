package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
)

var (
	errNoServer    = errors.New("match has no server assigned")
	errStopTimeout = errors.New("the bot did not release the match; is it running and connected to the same Redis?")
)

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

func (s *server) controlEnabled(w http.ResponseWriter) bool {
	if s.cmd == nil {
		writeError(w, http.StatusServiceUnavailable, "live control disabled: Redis is not configured")
		return false
	}
	return true
}

// sendStop pushes the command and waits for the bot to clear enable, which is
// how it reports that it let the server go.
func (s *server) sendStop(ctx context.Context, m *store.Match, action string) error {
	if m.IP == "" {
		return errNoServer
	}
	if err := s.cmd.Send(ctx, m.ID, action, "", m.IP, m.ConfigAuthKey); err != nil {
		return err
	}
	deadline := time.Now().Add(s.stopTimeout)
	for {
		cur, err := s.store.GetMatch(ctx, m.ID)
		if err != nil {
			return err
		}
		if !cur.Enable {
			return nil
		}
		if time.Now().After(deadline) {
			return errStopTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.stopPoll):
		}
	}
}

func (s *server) controlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNoServer):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errStopTimeout):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	default:
		storeError(w, err, "match not found")
	}
}

// running is true when the bot holds the match.
func running(m *store.Match) bool { return m.Enable && m.Status >= store.StatusStarting }

func (s *server) stopMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.controlEnabled(w) {
		return
	}
	m, err := s.store.GetMatch(r.Context(), id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	if !running(m) {
		writeError(w, http.StatusConflict, "match is not running")
		return
	}
	action := ebotcmd.ActionStop
	if r.URL.Query().Get("restart") == "false" {
		action = ebotcmd.ActionStopNoRestart
	}
	if err := s.sendStop(r.Context(), m, action); err != nil {
		s.controlError(w, err)
		return
	}
	s.writeMatch(w, r.Context(), http.StatusOK, id)
}

func resettable(m *store.Match) bool {
	return !m.Enable && m.Status > store.StatusNotStarted && m.Status < store.StatusEndMatch
}

func (s *server) resetMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMatch(r.Context(), id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	if !resettable(m) {
		writeError(w, http.StatusConflict, "only a stopped, unfinished match can be reset")
		return
	}
	if err := s.store.ResetMatch(r.Context(), id); err != nil {
		storeError(w, err, "match not found")
		return
	}
	s.writeMatch(w, r.Context(), http.StatusOK, id)
}

// restartMatch stops with restart, resets, then starts on the same server.
func (s *server) restartMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !s.controlEnabled(w) {
		return
	}
	ctx := r.Context()
	m, err := s.store.GetMatch(ctx, id)
	if err != nil {
		storeError(w, err, "match not found")
		return
	}
	if running(m) {
		if err := s.sendStop(ctx, m, ebotcmd.ActionStop); err != nil {
			s.controlError(w, err)
			return
		}
		if m, err = s.store.GetMatch(ctx, id); err != nil {
			storeError(w, err, "match not found")
			return
		}
	}
	if resettable(m) {
		if err := s.store.ResetMatch(ctx, id); err != nil {
			storeError(w, err, "match not found")
			return
		}
	}
	if m.ServerID == nil {
		writeError(w, http.StatusConflict, errNoServer.Error())
		return
	}
	if err := s.store.StartMatch(ctx, id, *m.ServerID); err != nil {
		storeError(w, err, "match not found")
		return
	}
	s.writeMatch(w, ctx, http.StatusOK, id)
}
