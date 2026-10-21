// Package config loads the process configuration from environment variables.
// Every variable can also be supplied as NAME_FILE, pointing at a file that
// holds the value (Docker and Kubernetes secrets).
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
)

const DefaultMaps = "de_dust2,de_mirage,de_inferno,de_nuke,de_ancient,de_anubis,de_overpass,de_vertigo"

type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	RolesClaim   string
	// RoleMap is the raw "claimValue=role,claimValue=role" list.
	RoleMap string
	// DefaultRole is the role for users that match no RoleMap entry. "deny" rejects them.
	DefaultRole string
}

type MatchDefaults struct {
	MaxRound           int
	Rules              string
	OvertimeEnable     bool
	OvertimeMaxRound   int
	OvertimeStartMoney int
	Password           string
}

type Config struct {
	Addr    string
	Title   string
	Version string

	DBDSN string

	RedisAddr     string
	RedisUsername string
	RedisPassword string
	RedisFromWS   string
	RedisToWS     string

	SessionKey         []byte
	InsecureCookies    bool
	AdminUser          string
	AdminPassword      string
	LocalLoginDisabled bool
	OIDC               OIDC
	// LoginProviders is the raw JSON list of SSO buttons: [{"name","hint","icon"}].
	LoginProviders string

	DemoPath          string
	DemoDownload      bool
	LogMatchPath      string
	LogMatchAdminPath string

	// Mode is "net" (show the GOTV address) or "lan" (show the server address).
	Mode        string
	RefreshTime int
	Maps        []string
	Rules       []string
	Defaults    MatchDefaults

	APIKeys []string

	LogLevel  string
	LogFormat string
}

// Load reads the configuration through getenv. Tests pass a map lookup;
// the server passes os.Getenv.
func Load(getenv func(string) string) (*Config, error) {
	e := &env{get: getenv}

	c := &Config{
		Addr:    e.str("HTTP_ADDR", ":8080"),
		Title:   e.str("EBOT_TITLE", "eBot"),
		Version: e.str("EBOT_VERSION", ""),

		RedisAddr:     e.str("EBOT_REDIS_ADDR", "127.0.0.1:6379"),
		RedisUsername: e.str("EBOT_REDIS_USERNAME", ""),
		RedisPassword: e.str("EBOT_REDIS_PASSWORD", ""),
		RedisFromWS:   e.str("EBOT_REDIS_FROM_WS", "ebot-from-ws"),
		RedisToWS:     e.str("EBOT_REDIS_TO_WS", "ebot-to-ws"),

		InsecureCookies:    e.boolean("EBOT_INSECURE_COOKIES", false),
		AdminUser:          e.str("EBOT_ADMIN_USER", ""),
		AdminPassword:      e.str("EBOT_ADMIN_PASSWORD", ""),
		LocalLoginDisabled: e.boolean("EBOT_LOCAL_LOGIN_DISABLED", false),
		OIDC: OIDC{
			Issuer:       e.str("EBOT_OIDC_ISSUER", ""),
			ClientID:     e.str("EBOT_OIDC_CLIENT_ID", ""),
			ClientSecret: e.str("EBOT_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  e.str("EBOT_OIDC_REDIRECT_URL", ""),
			RolesClaim:   e.str("EBOT_OIDC_ROLES_CLAIM", "roles"),
			RoleMap:      e.str("EBOT_OIDC_ROLE_MAP", ""),
			DefaultRole:  e.str("EBOT_OIDC_DEFAULT_ROLE", "deny"),
		},
		LoginProviders: e.str("EBOT_LOGIN_PROVIDERS", ""),

		DemoPath:          e.str("EBOT_DEMO_PATH", "/demos"),
		DemoDownload:      e.boolean("EBOT_DEMO_DOWNLOAD", true),
		LogMatchPath:      e.str("EBOT_LOG_MATCH_PATH", "/logs/log_match"),
		LogMatchAdminPath: e.str("EBOT_LOG_MATCH_ADMIN_PATH", "/logs/log_match_admin"),

		Mode:        e.str("EBOT_MODE", "net"),
		RefreshTime: e.integer("EBOT_REFRESH_TIME", 30),
		Maps:        splitCSV(e.str("EBOT_MAPS", DefaultMaps)),
		Rules:       splitCSV(e.str("EBOT_RULES", "rules")),
		Defaults: MatchDefaults{
			MaxRound:           e.integer("EBOT_DEFAULT_MAX_ROUND", 12),
			Rules:              e.str("EBOT_DEFAULT_RULES", "rules"),
			OvertimeEnable:     e.boolean("EBOT_DEFAULT_OVERTIME_ENABLE", false),
			OvertimeMaxRound:   e.integer("EBOT_DEFAULT_OVERTIME_MAX_ROUND", 3),
			OvertimeStartMoney: e.integer("EBOT_DEFAULT_OVERTIME_START_MONEY", 10000),
			Password:           e.str("EBOT_DEFAULT_PASSWORD", ""),
		},

		APIKeys: splitCSV(e.str("EBOT_API_KEYS", "")),

		LogLevel:  e.str("EBOT_LOG_LEVEL", "info"),
		LogFormat: e.str("EBOT_LOG_FORMAT", "text"),
	}

	c.DBDSN = e.str("EBOT_DB_DSN", "")
	if c.DBDSN == "" {
		my := mysql.NewConfig()
		my.User = e.str("EBOT_DB_USER", "ebot")
		my.Passwd = e.str("EBOT_DB_PASSWORD", "")
		my.Net = "tcp"
		my.Addr = e.str("EBOT_DB_HOST", "127.0.0.1") + ":" + e.str("EBOT_DB_PORT", "3306")
		my.DBName = e.str("EBOT_DB_NAME", "ebotv3")
		my.ParseTime = true
		my.Loc = nil
		c.DBDSN = my.FormatDSN()
	}

	key := e.str("EBOT_SESSION_KEY", "")
	switch {
	case len(key) >= 32:
		c.SessionKey = []byte(key)
	case key != "":
		e.fail("EBOT_SESSION_KEY is set but too short (%d bytes); it must be at least 32 bytes", len(key))
	case c.InsecureCookies:
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			e.fail("generate session key: %v", err)
		}
		c.SessionKey = b
	default:
		e.fail("EBOT_SESSION_KEY is required (at least 32 bytes); set EBOT_INSECURE_COOKIES=true only for local development")
	}

	if c.Mode != "net" && c.Mode != "lan" {
		e.fail("EBOT_MODE must be net or lan, got %q", c.Mode)
	}
	if c.OIDC.Issuer != "" {
		for name, v := range map[string]string{
			"EBOT_OIDC_CLIENT_ID":     c.OIDC.ClientID,
			"EBOT_OIDC_CLIENT_SECRET": c.OIDC.ClientSecret,
			"EBOT_OIDC_REDIRECT_URL":  c.OIDC.RedirectURL,
		} {
			if v == "" {
				e.fail("%s is required when EBOT_OIDC_ISSUER is set", name)
			}
		}
	}
	if c.LocalLoginDisabled && c.OIDC.Issuer == "" {
		e.fail("EBOT_LOCAL_LOGIN_DISABLED needs EBOT_OIDC_ISSUER, otherwise nobody can log in")
	}
	if len(c.Maps) == 0 {
		e.fail("EBOT_MAPS must list at least one map")
	}

	if err := e.err(); err != nil {
		return nil, err
	}
	return c, nil
}

type env struct {
	get  func(string) string
	errs []error
}

func (e *env) fail(format string, args ...any) {
	e.errs = append(e.errs, fmt.Errorf(format, args...))
}

func (e *env) err() error { return errors.Join(e.errs...) }

func (e *env) str(name, def string) string {
	if v := e.get(name); v != "" {
		return v
	}
	if path := e.get(name + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			e.fail("%s_FILE: %v", name, err)
			return def
		}
		return strings.TrimRight(string(b), "\r\n")
	}
	return def
}

func (e *env) boolean(name string, def bool) bool {
	v := e.str(name, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.fail("%s: %q is not a boolean", name, v)
		return def
	}
	return b
}

func (e *env) integer(name string, def int) int {
	v := e.str(name, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.fail("%s: %q is not a number", name, v)
		return def
	}
	return n
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
