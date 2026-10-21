// Package live fans the bot's Redis pub/sub messages out to browsers as
// server-sent events.
package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
)

const (
	scopeMatch  = "match"
	scopeRcon   = "rcon"
	scopeLogger = "logger"
	scopeAlive  = "alive"

	clientBuffer = 64
)

type client struct {
	scope string
	id    string
	ch    chan string
}

type Hub struct {
	channel string
	rdb     *redis.Client
	cmd     *ebotcmd.Publisher

	heartbeat time.Duration
	keepalive time.Duration
	backoff   time.Duration

	// redisUp is true while the subscription is established. botSeen is the
	// unix time in nanoseconds of the last message from the bot, lastProbe
	// the last time an alive check was sent.
	redisUp   atomic.Bool
	botSeen   atomic.Int64
	lastProbe atomic.Int64
	botTTL    time.Duration
	probeGap  time.Duration

	mu      sync.Mutex
	clients map[*client]struct{}
	loggers int
	alive   int
}

func New(cfg *config.Config, rdb *redis.Client, cmd *ebotcmd.Publisher) *Hub {
	return &Hub{
		channel:   cfg.RedisToWS,
		rdb:       rdb,
		cmd:       cmd,
		heartbeat: 15 * time.Second,
		keepalive: 30 * time.Second,
		backoff:   time.Second,
		botTTL:    20 * time.Second,
		probeGap:  10 * time.Second,
		clients:   map[*client]struct{}{},
	}
}

// Run subscribes to the to-websocket channel and blocks until ctx ends.
func (h *Hub) Run(ctx context.Context) error {
	go h.keepaliveLoop(ctx)

	delay := h.backoff
	for ctx.Err() == nil {
		err := h.subscribeOnce(ctx, func() { delay = h.backoff })
		if ctx.Err() != nil {
			break
		}
		slog.Warn("live: redis subscription lost", "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	return nil
}

func (h *Hub) subscribeOnce(ctx context.Context, onReady func()) error {
	ps := h.rdb.Subscribe(ctx, h.channel)
	defer ps.Close()
	stop := context.AfterFunc(ctx, func() { _ = ps.Close() })
	defer stop()
	if _, err := ps.Receive(ctx); err != nil {
		return err
	}
	h.redisUp.Store(true)
	defer h.redisUp.Store(false)
	onReady()
	for {
		msg, err := ps.ReceiveMessage(ctx)
		if err != nil {
			return err
		}
		h.handle(msg.Payload)
	}
}

type envelope struct {
	Scope string `json:"scope"`
	Data  string `json:"data"`
}

type inner struct {
	Message string          `json:"message"`
	ID      json.RawMessage `json:"id"`
}

func (i inner) id() string {
	return strings.Trim(string(i.ID), `"`)
}

func (h *Hub) handle(payload string) {
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return
	}
	h.botSeen.Store(time.Now().UnixNano())
	var in inner
	parsed := json.Unmarshal([]byte(env.Data), &in) == nil
	if parsed && in.Message == "ping" {
		return
	}
	switch env.Scope {
	case scopeMatch, scopeAlive:
		h.dispatch(env.Scope, "", env.Data)
	case scopeRcon, scopeLogger:
		if parsed && in.id() != "" {
			h.dispatch(env.Scope, in.id(), env.Data)
		}
	}
}

func (h *Hub) dispatch(scope, id, data string) {
	var slow []*client
	h.mu.Lock()
	for c := range h.clients {
		if c.scope != scope || c.id != id {
			continue
		}
		select {
		case c.ch <- data:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.Unlock()
	for _, c := range slow {
		h.remove(c)
	}
}

func (h *Hub) add(ctx context.Context, c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	first := false
	switch c.scope {
	case scopeLogger:
		h.loggers++
		first = h.loggers == 1
	case scopeAlive:
		h.alive++
		first = true
	}
	h.mu.Unlock()
	if !first {
		return
	}
	if c.scope == scopeLogger {
		h.push(ctx, "__true__")
	} else {
		h.push(ctx, "__aliveCheck__")
	}
}

// remove is idempotent. It closes the client's channel, which ends its handler.
func (h *Hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	close(c.ch)
	last := false
	switch c.scope {
	case scopeLogger:
		h.loggers--
		last = h.loggers == 0
	case scopeAlive:
		h.alive--
	}
	h.mu.Unlock()
	if last {
		h.push(context.Background(), "__false__")
	}
}

func (h *Hub) push(ctx context.Context, raw string) {
	if h.cmd == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.cmd.Push(ctx, raw); err != nil {
		slog.Warn("live: push to bot failed", "msg", raw, "err", err)
	}
}

func (h *Hub) keepaliveLoop(ctx context.Context) {
	t := time.NewTicker(h.keepalive)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.mu.Lock()
		loggers, alive := h.loggers, h.alive
		h.mu.Unlock()
		if loggers > 0 {
			h.push(ctx, "__true__")
		}
		if alive > 0 {
			h.push(ctx, "__aliveCheck__")
		}
	}
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request, scope, id string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	c := &client{scope: scope, id: id, ch: make(chan string, clientBuffer)}
	h.add(r.Context(), c)
	defer h.remove(c)

	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()

	t := time.NewTicker(h.heartbeat)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
		case data, ok := <-c.ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte(frame(scope, data))); err != nil {
				return
			}
		}
		fl.Flush()
	}
}

func frame(event, data string) string {
	var b strings.Builder
	b.WriteString("event: " + event + "\n")
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func matchID(w http.ResponseWriter, r *http.Request) (string, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n <= 0 {
		http.Error(w, "invalid match id", http.StatusBadRequest)
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}

// Matches streams scope "match" messages. Public.
func (h *Hub) Matches(w http.ResponseWriter, r *http.Request) { h.serve(w, r, scopeMatch, "") }

// Rcon streams scope "rcon" messages for the match in r.PathValue("id"). Admin.
func (h *Hub) Rcon(w http.ResponseWriter, r *http.Request) {
	if id, ok := matchID(w, r); ok {
		h.serve(w, r, scopeRcon, id)
	}
}

// Logger streams scope "logger" messages for r.PathValue("id"). Admin.
func (h *Hub) Logger(w http.ResponseWriter, r *http.Request) {
	if id, ok := matchID(w, r); ok {
		h.serve(w, r, scopeLogger, id)
	}
}

// Alive streams scope "alive" messages. Admin.
func (h *Hub) Alive(w http.ResponseWriter, r *http.Request) { h.serve(w, r, scopeAlive, "") }
