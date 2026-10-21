package ebotcmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestValidAction(t *testing.T) {
	accepted := []string{
		"stop", "stopNoRs", "executeCommand", "passknife", "forceknife",
		"forceknifeend", "forcestart", "stopback", "pauseunpause", "fixsides",
		"streamerready", "goBackRounds", "skipmap",
	}
	for _, a := range accepted {
		if !ValidAction(a) {
			t.Errorf("%q should be accepted", a)
		}
	}
	for _, a := range []string{"skipmapprev", "skipmapnext", "", "stop; flushall", "STOP", " stop", "stop "} {
		if ValidAction(a) {
			t.Errorf("%q should be rejected", a)
		}
	}
}

func TestSend(t *testing.T) {
	mr := miniredis.RunT(t)
	p := NewPublisher(mr.Addr(), "", "", "from-ws")
	t.Cleanup(func() { p.Close() })
	ctx := context.Background()

	cases := []struct {
		id        int64
		action    string
		arg       string
		ip        string
		authkey   string
		wantPlain string
	}{
		{12, "stop", "", "10.0.0.1:27015", "key-one", "12 stop 10.0.0.1:27015"},
		{7, "executeCommand", "say hello world", "10.0.0.2:27016", "key-two", "7 executeCommand 10.0.0.2:27016 say hello world"},
		{3, "goBackRounds", "5", "10.0.0.3:27015", "key-three", "3 goBackRounds 10.0.0.3:27015 5"},
	}
	for _, c := range cases {
		if err := p.Send(ctx, c.id, c.action, c.arg, c.ip, c.authkey); err != nil {
			t.Fatal(err)
		}
		list, err := mr.List("from-ws")
		if err != nil || len(list) == 0 {
			t.Fatalf("list: %v %v", list, err)
		}
		var msg []string
		if err := json.Unmarshal([]byte(list[0]), &msg); err != nil {
			t.Fatalf("not a JSON string array: %q", list[0])
		}
		if len(msg) != 2 || msg[1] != c.ip {
			t.Fatalf("message = %v", msg)
		}
		got, err := Decrypt(msg[0], c.authkey)
		if err != nil || got != c.wantPlain {
			t.Errorf("plaintext = %q (%v), want %q", got, err, c.wantPlain)
		}
	}
}

func TestSendRejectsUnknownAction(t *testing.T) {
	mr := miniredis.RunT(t)
	p := NewPublisher(mr.Addr(), "", "", "from-ws")
	t.Cleanup(func() { p.Close() })
	if err := p.Send(context.Background(), 1, "flushall", "", "1.2.3.4:27015", "k"); err == nil {
		t.Error("expected error")
	}
	if mr.Exists("from-ws") {
		t.Error("nothing should be pushed")
	}
}

func TestPush(t *testing.T) {
	mr := miniredis.RunT(t)
	p := NewPublisher(mr.Addr(), "", "", "from-ws")
	t.Cleanup(func() { p.Close() })
	if err := p.Push(context.Background(), "__aliveCheck__"); err != nil {
		t.Fatal(err)
	}
	if l, _ := mr.List("from-ws"); len(l) != 1 || l[0] != "__aliveCheck__" {
		t.Errorf("list = %v", l)
	}
}
