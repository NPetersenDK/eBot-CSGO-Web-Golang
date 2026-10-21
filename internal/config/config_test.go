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

func TestIsCurrentMap(t *testing.T) {
	for name, want := range map[string]bool{
		"CurrentMap": true, "currentmap": true, "CurrentMap-workshop_x": true,
		"de_dust2": false, "tba": false, "": false,
	} {
		if got := IsCurrentMap(name); got != want {
			t.Errorf("IsCurrentMap(%q) = %v", name, got)
		}
	}
}

func TestMatchDefaultsFromEnv(t *testing.T) {
	c, err := Load(lookup(map[string]string{
		"EBOT_SESSION_KEY":      key32,
		"EBOT_DEFAULT_PASSWORD": "esb",
		"EBOT_DEFAULT_RULES":    "gamemode_competitive",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Defaults.Password != "esb" || c.Defaults.Rules != "gamemode_competitive" {
		t.Fatalf("defaults = %+v", c.Defaults)
	}
}

func TestMigrateDefaults(t *testing.T) {
	c, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.AutoMigrate || c.DBWait.Seconds() != 60 {
		t.Fatalf("auto migrate %v, wait %v", c.AutoMigrate, c.DBWait)
	}
	c, err = Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_AUTO_MIGRATE": "false", "EBOT_DB_WAIT": "5"}))
	if err != nil || c.AutoMigrate || c.DBWait.Seconds() != 5 {
		t.Fatalf("overrides: %+v %v", c, err)
	}
}

func TestTimezone(t *testing.T) {
	c, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32}))
	if err != nil || c.Location.String() != "UTC" {
		t.Fatalf("default: %v %v", c.Location, err)
	}
	c, err = Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_TIMEZONE": "Europe/Copenhagen"}))
	if err != nil || c.Location.String() != "Europe/Copenhagen" {
		t.Fatalf("zone: %v %v", c.Location, err)
	}
	if _, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": key32, "EBOT_TIMEZONE": "Mars/Base"})); err == nil {
		t.Fatal("an unknown zone must fail at start")
	}
}

func TestPlaceholderSessionKeyIsRefused(t *testing.T) {
	_, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": "change-me-32-chars-minimum-replace-this-value"}))
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Load(lookup(map[string]string{"EBOT_SESSION_KEY": "CHANGE-ME-abcdefghijklmnopqrstuvwxyz"})); err == nil {
		t.Fatal("the check must ignore case")
	}
}
