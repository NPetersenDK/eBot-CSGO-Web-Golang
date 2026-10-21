package live

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
)

type env struct {
	mr  *miniredis.Miniredis
	hub *Hub
	srv *httptest.Server
}

func setup(t *testing.T) *env {
	t.Helper()
	mr := miniredis.RunT(t)
	cfg := &config.Config{RedisAddr: mr.Addr(), RedisToWS: "to-ws", RedisFromWS: "from-ws"}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	pub := ebotcmd.NewPublisher(mr.Addr(), "", "", "from-ws")
	t.Cleanup(func() { pub.Close() })

	h := New(cfg, rdb, pub)
	h.heartbeat = 50 * time.Millisecond
	h.keepalive = 100 * time.Millisecond
	h.backoff = 10 * time.Millisecond

	mux := http.NewServeMux()
	mux.HandleFunc("GET /live/matches", h.Matches)
	mux.HandleFunc("GET /admin/live/alive", h.Alive)
	mux.HandleFunc("GET /admin/live/{id}/rcon", h.Rcon)
	mux.HandleFunc("GET /admin/live/{id}/logger", h.Logger)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Run did not return")
		}
	})
	waitFor(t, func() bool { return mr.PubSubNumSub("to-ws")["to-ws"] == 1 })
	return &env{mr, h, srv}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

type stream struct {
	cancel context.CancelFunc
	lines  chan string
	resp   *http.Response
}

func (e *env) open(t *testing.T, path string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	s := &stream{cancel: cancel, lines: make(chan string, 1024), resp: resp}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	t.Cleanup(cancel)
	return s
}

func (s *stream) next(t *testing.T, want string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-s.lines:
			if !ok {
				t.Fatalf("stream closed waiting for %q", want)
			}
			if l == want {
				return
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %q", want)
		}
	}
}

func (s *stream) none(t *testing.T, unwanted string, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case l := <-s.lines:
			if l == unwanted {
				t.Fatalf("unexpected line %q", l)
			}
		case <-timeout:
			return
		}
	}
}

func (e *env) publish(t *testing.T, scope, data string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"scope": scope, "data": data})
	e.mr.Publish("to-ws", string(b))
}

func TestHeadersAndMatchRouting(t *testing.T) {
	e := setup(t)
	s := e.open(t, "/live/matches")
	if got := s.resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content type %q", got)
	}
	if s.resp.Header.Get("Cache-Control") != "no-cache" || s.resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers: %v", s.resp.Header)
	}
	s.next(t, ": connected")

	body := `{"message":"status","content":"Finished","id":5}`
	e.publish(t, "rcon", `{"id":5,"content":"x"}`)
	e.publish(t, "match", body)
	s.next(t, "event: match")
	s.next(t, "data: "+body)
}

func TestPingDropped(t *testing.T) {
	e := setup(t)
	s := e.open(t, "/live/matches")
	s.next(t, ": connected")
	e.publish(t, "match", `{"message":"ping"}`)
	e.publish(t, "match", `{"message":"status","id":1}`)
	s.next(t, "event: match")
	s.next(t, `data: {"message":"status","id":1}`)
	s.none(t, `data: {"message":"ping"}`, 100*time.Millisecond)
}

func TestRconAndLoggerRoutedById(t *testing.T) {
	e := setup(t)
	r7 := e.open(t, "/admin/live/7/rcon")
	r8 := e.open(t, "/admin/live/8/rcon")
	l7 := e.open(t, "/admin/live/7/logger")
	r7.next(t, ": connected")
	r8.next(t, ": connected")
	l7.next(t, ": connected")

	e.publish(t, "rcon", `{"id":"7","content":"seven"}`)
	e.publish(t, "rcon", `{"id":8,"content":"eight"}`)
	e.publish(t, "logger", `{"id":7,"content":"log"}`)

	r7.next(t, "event: rcon")
	r7.next(t, `data: {"id":"7","content":"seven"}`)
	r8.next(t, `data: {"id":8,"content":"eight"}`)
	l7.next(t, "event: logger")
	l7.next(t, `data: {"id":7,"content":"log"}`)
	r7.none(t, `data: {"id":8,"content":"eight"}`, 100*time.Millisecond)
	l7.none(t, `data: {"id":"7","content":"seven"}`, 10*time.Millisecond)
}

func TestInvalidID(t *testing.T) {
	e := setup(t)
	for _, p := range []string{"/admin/live/abc/rcon", "/admin/live/0/logger", "/admin/live/-1/rcon"} {
		resp, err := http.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("%s: status %d", p, resp.StatusCode)
		}
	}
}

func (e *env) pushed() []string {
	l, _ := e.mr.List("from-ws")
	return l
}

func count(l []string, v string) int {
	n := 0
	for _, x := range l {
		if x == v {
			n++
		}
	}
	return n
}

func TestLoggerRefcount(t *testing.T) {
	e := setup(t)
	a := e.open(t, "/admin/live/1/logger")
	a.next(t, ": connected")
	waitFor(t, func() bool { return count(e.pushed(), "__true__") >= 1 })
	b := e.open(t, "/admin/live/2/logger")
	b.next(t, ": connected")

	a.cancel()
	waitFor(t, func() bool { e.hub.mu.Lock(); defer e.hub.mu.Unlock(); return e.hub.loggers == 1 })
	if n := count(e.pushed(), "__false__"); n != 0 {
		t.Fatalf("__false__ pushed with a logger left (%d)", n)
	}
	b.cancel()
	waitFor(t, func() bool { return count(e.pushed(), "__false__") == 1 })

	// Keepalive repeats __true__ only while a logger is connected.
	before := count(e.pushed(), "__true__")
	time.Sleep(250 * time.Millisecond)
	if count(e.pushed(), "__true__") != before {
		t.Error("__true__ pushed with no logger clients")
	}
	c := e.open(t, "/admin/live/3/logger")
	c.next(t, ": connected")
	waitFor(t, func() bool { return count(e.pushed(), "__true__") >= before+3 })
}

func TestAliveCheck(t *testing.T) {
	e := setup(t)
	s := e.open(t, "/admin/live/alive")
	s.next(t, ": connected")
	waitFor(t, func() bool { return count(e.pushed(), "__aliveCheck__") >= 3 })
	e.publish(t, "alive", "__isAlive__")
	s.next(t, "event: alive")
	s.next(t, "data: __isAlive__")
	s.cancel()
	waitFor(t, func() bool { e.hub.mu.Lock(); defer e.hub.mu.Unlock(); return e.hub.alive == 0 })
}

func TestHeartbeat(t *testing.T) {
	e := setup(t)
	s := e.open(t, "/live/matches")
	s.next(t, ": ping")
	s.next(t, ": ping")
}

func TestSlowClientDoesNotBlock(t *testing.T) {
	e := setup(t)
	slow := &client{scope: scopeLogger, id: "9", ch: make(chan string, 1)}
	e.hub.add(context.Background(), slow)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			e.hub.dispatch(scopeLogger, "9", "x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch blocked on a slow client")
	}
	e.hub.mu.Lock()
	_, still := e.hub.clients[slow]
	e.hub.mu.Unlock()
	if still {
		t.Error("slow client was not dropped")
	}
	waitFor(t, func() bool { return count(e.pushed(), "__false__") == 1 })
}

func TestReconnectsAfterRedisRestart(t *testing.T) {
	e := setup(t)
	s := e.open(t, "/live/matches")
	s.next(t, ": connected")
	e.mr.Close()
	time.Sleep(50 * time.Millisecond)
	if err := e.mr.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.mr.PubSubNumSub("to-ws")["to-ws"] == 1 })
	e.publish(t, "match", `{"message":"status","id":2}`)
	s.next(t, `data: {"message":"status","id":2}`)
}

func TestFrameMultiline(t *testing.T) {
	got := frame("rcon", "a\nb")
	if got != "event: rcon\ndata: a\ndata: b\n\n" || strings.Count(got, "\n\n") != 1 {
		t.Errorf("frame = %q", got)
	}
}
