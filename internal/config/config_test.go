package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lookup(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const key32 = "0123456789abcdef0123456789abcdef"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.RedisFromWS != "ebot-from-ws" || c.Defaults.MaxRound != 12 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if len(c.Maps) < 5 || c.Maps[0] != "de_dust2" {
		t.Fatalf("maps = %v", c.Maps)
	}
	if !strings.Contains(c.DBDSN, "ebotv3") || !strings.Contains(c.DBDSN, "parseTime=true") {
		t.Fatalf("dsn = %s", c.DBDSN)
	}
}

func TestMapsFromEnv(t *testing.T) {
	c, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_MAPS": " de_a , de_b,,"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Maps) != 2 || c.Maps[1] != "de_b" {
		t.Fatalf("maps = %v", c.Maps)
	}
}

func TestSessionKeyRequired(t *testing.T) {
	if _, err := Load(lookup(nil)); err == nil {
		t.Fatal("expected an error without a session key")
	}
	if _, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": "short"})); err == nil {
		t.Fatal("expected an error for a short key")
	}
	c, err := Load(lookup(map[string]string{"EBOT_INSECURE_COOKIES": "true"}))
	if err != nil || len(c.SessionKey) != 32 {
		t.Fatalf("insecure dev key: %v %v", err, c)
	}
}

func TestFileVariant(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(key32+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY_FILE": p}))
	if err != nil || string(c.SessionKey) != key32 {
		t.Fatalf("file key: %v %v", err, c)
	}
}

func TestOIDCNeedsClient(t *testing.T) {
	_, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_OIDC_ISSUER": "https://idp"}))
	if err == nil || !strings.Contains(err.Error(), "EBOT_OIDC_CLIENT_ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestLocalLoginNeedsAlternative(t *testing.T) {
	_, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_LOCAL_LOGIN_DISABLED": "true"}))
	if err == nil {
		t.Fatal("expected an error")
	}
}
