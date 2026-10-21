package live

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Status is the health of the live pipeline.
//
// Websocket is true while this panel is subscribed to the bot's Redis channel,
// which is what carries every live update and reply. EBot is true while the
// bot has sent something recently. The bot pings every 5 seconds, so silence
// for botTTL means it is down or stuck.
type Status struct {
	OK        bool `json:"ok"`
	Websocket bool `json:"websocket"`
	EBot      bool `json:"ebot"`
}

// Status reports the current health. When the bot has been quiet it also asks
// it to answer, at most once per probeGap, so a bot that does not ping still
// shows up on the next check.
func (h *Hub) Status() Status {
	s := Status{Websocket: h.redisUp.Load()}
	if s.Websocket {
		seen := time.Unix(0, h.botSeen.Load())
		s.EBot = h.botSeen.Load() != 0 && time.Since(seen) < h.botTTL
		if !s.EBot {
			h.probe()
		}
	}
	s.OK = s.Websocket && s.EBot
	return s
}

func (h *Hub) probe() {
	now := time.Now().UnixNano()
	last := h.lastProbe.Load()
	if last != 0 && time.Duration(now-last) < h.probeGap {
		return
	}
	if !h.lastProbe.CompareAndSwap(last, now) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h.push(ctx, "__aliveCheck__")
}

// StatusHandler serves Status as JSON.
func (h *Hub) StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(h.Status())
}
