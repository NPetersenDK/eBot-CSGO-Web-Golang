package store

import (
	"errors"
	"testing"
)

func TestCheckServerAddress(t *testing.T) {
	for addr, ok := range map[string]bool{
		"10.0.0.5:27015":       true,
		"gs.example.com:27015": true,
		"10.0.0.5":             false,
		"10.0.0.5:0":           false,
		"10.0.0.5:70000":       false,
		"999.0.0.1:27015":      false,
		`10.0.0.5";quit:27015`: false,
		"a b.example.com:1":    false,
		"gs.example.com;x:1":   false,
		"localhost:27015":      false,
		":27015":               false,
	} {
		err := CheckServerAddress(addr)
		if (err == nil) != ok {
			t.Errorf("CheckServerAddress(%q) = %v, want ok=%v", addr, err, ok)
		}
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckServerAddress(%q): %v is not ErrInvalid", addr, err)
		}
	}
}

func TestCheckServerText(t *testing.T) {
	for s, ok := range map[string]bool{"Server #1": true, "": true, `a"b`: false, "a;b": false, "a\nb": false, "a\\b": false} {
		if err := CheckServerText("Name", s); (err == nil) != ok {
			t.Errorf("CheckServerText(%q) = %v", s, err)
		}
	}
}

func TestExpandServersRejectsUnsafeText(t *testing.T) {
	base := ServerCreate{Mode: "single", Host: "10.0.0.1", Port: 27015, Hostname: "ok"}
	if _, err := expandServers(base); err != nil {
		t.Fatal(err)
	}
	for _, in := range []ServerCreate{
		{Mode: "single", Host: "10.0.0.1", Port: 1, Hostname: `x"; quit`},
		{Mode: "single", Host: "10.0.0.1", Port: 1, Hostname: "x", TVIP: "1.1.1.1;quit"},
	} {
		if _, err := expandServers(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("expandServers(%+v) = %v, want ErrInvalid", in, err)
		}
	}
}
