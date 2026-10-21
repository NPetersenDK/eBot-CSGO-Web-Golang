package live

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusFollowsBotMessages(t *testing.T) {
	e := setup(t)
	e.hub.botTTL = 300 * time.Millisecond
	e.hub.probeGap = 20 * time.Millisecond

	if s := e.hub.Status(); !s.Websocket || s.EBot || s.OK {
		t.Fatalf("before any bot message: %+v", s)
	}

	e.publish(t, "alive", `{"message":"ping"}`)
	waitFor(t, func() bool { return e.hub.Status().OK })

	time.Sleep(400 * time.Millisecond)
	if s := e.hub.Status(); s.EBot || s.OK || !s.Websocket {
		t.Fatalf("after silence: %+v", s)
	}
}

func TestStatusAsksTheBotWhenQuiet(t *testing.T) {
	e := setup(t)
	e.hub.probeGap = time.Hour

	e.hub.Status()
	e.hub.Status()
	e.hub.Status()
	if n := count(e.pushed(), "__aliveCheck__"); n != 1 {
		t.Fatalf("alive checks sent = %d, want 1 per probe gap", n)
	}
}

func TestStatusAnswerCountsAsAlive(t *testing.T) {
	e := setup(t)
	e.publish(t, "alive", `__isAlive__`)
	waitFor(t, func() bool { return e.hub.Status().EBot })
}

func TestStatusRedisDown(t *testing.T) {
	e := setup(t)
	e.publish(t, "alive", `{"message":"ping"}`)
	waitFor(t, func() bool { return e.hub.Status().OK })

	e.mr.Close()
	waitFor(t, func() bool { return !e.hub.Status().Websocket })
	if s := e.hub.Status(); s.EBot || s.OK {
		t.Fatalf("with redis down: %+v", s)
	}
}

func TestStatusHandlerJSON(t *testing.T) {
	e := setup(t)
	rec := httptest.NewRecorder()
	e.hub.StatusHandler(rec, httptest.NewRequest("GET", "/admin/status", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache control %q", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"websocket":true`) || !strings.Contains(body, `"ebot":false`) {
		t.Errorf("body %s", body)
	}
}
