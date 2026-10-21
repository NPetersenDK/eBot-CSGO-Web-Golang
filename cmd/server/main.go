// Command server is eBot Web: the match admin panel for the eBot CS bot.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	ebotweb "github.com/NPetersenDK/eBot-CSGO-Web-Golang"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/api"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/auth"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/ebotcmd"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/live"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/store"
	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/web"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.Version == "" {
		cfg.Version = version
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DBDSN)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := waitForDB(ctx, st, cfg.DBWait, log); err != nil {
		return err
	}
	if cfg.AutoMigrate {
		res, err := st.Migrate(ctx)
		if err != nil {
			return fmt.Errorf("create the database schema (set EBOT_AUTO_MIGRATE=false to skip): %w", err)
		}
		if len(res.Created) > 0 {
			log.Info("created database tables", "count", len(res.Created))
		}
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Username: cfg.RedisUsername, Password: cfg.RedisPassword})
	defer rdb.Close()
	cmd := ebotcmd.NewPublisher(cfg.RedisAddr, cfg.RedisUsername, cfg.RedisPassword, cfg.RedisFromWS)
	defer cmd.Close()
	hub := live.New(cfg, rdb, cmd)

	assets, err := fs.Sub(ebotweb.FS, "web")
	if err != nil {
		return err
	}
	sessions := auth.NewManager(cfg.SessionKey, !cfg.InsecureCookies)
	app := &web.App{Cfg: cfg, Store: st, Auth: sessions, Cmd: cmd, Live: hub, Log: log}
	if err := web.NewApp(app, assets); err != nil {
		return err
	}

	if err := bootstrapAdmin(ctx, cfg, st); err != nil {
		return err
	}

	authHandler, err := auth.NewHandler(ctx, cfg, sessions, st, app.RenderStatus)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", readyz(st.Ping, func(ctx context.Context) error { return rdb.Ping(ctx).Err() }))
	authHandler.Register(mux)
	app.Register(mux)
	api.Register(mux, cfg, st, cmd)

	handler := securityHeaders(!cfg.InsecureCookies, originOf(cfg.OIDC.Issuer), sameOriginGuard(sessions.Middleware(st)(mux)))

	// Streaming handlers use the request context. Cancelling the base context
	// on shutdown ends them, so Shutdown does not wait for open event streams.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}

	go func() { _ = hub.Run(ctx) }()
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "version", cfg.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	cancelBase()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// waitForDB retries until MySQL accepts connections, so the panel can start
// together with the database in a compose file or a cluster.
func waitForDB(ctx context.Context, st *store.Store, wait time.Duration, log *slog.Logger) error {
	deadline := time.Now().Add(wait)
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := st.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("database not reachable after %s: %w", wait, err)
		}
		if attempt == 1 || attempt%5 == 0 {
			log.Warn("waiting for the database", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// bootstrapAdmin creates the first super admin when the user table is empty.
func bootstrapAdmin(ctx context.Context, cfg *config.Config, st *store.Store) error {
	if cfg.AdminUser == "" || cfg.AdminPassword == "" {
		return nil
	}
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = st.CreateUser(ctx, store.UserInput{
		FirstName: "Admin", LastName: "Admin", Email: cfg.AdminUser + "@localhost",
		Username: cfg.AdminUser, Password: cfg.AdminPassword, IsActive: true, IsSuperAdmin: true,
	})
	if err == nil {
		slog.Info("created bootstrap admin", "username", cfg.AdminUser)
	}
	return err
}

// readyz answers 503 without detail when a dependency fails. The error goes to the log.
func readyz(db, redis func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for name, ping := range map[string]func(context.Context) error{"database": db, "redis": redis} {
			if err := ping(ctx); err != nil {
				slog.Error("readiness check failed", "dependency", name, "err", err)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = w.Write([]byte("ok"))
	}
}

// contentSecurityPolicy builds the CSP. Chrome applies form-action to the
// redirect that answers a form post, so the single sign-on logout (a POST that
// is answered with a redirect to the identity provider) needs the provider's
// origin in the list.
func contentSecurityPolicy(formActionExtra string) string {
	formAction := "'self'"
	if formActionExtra != "" {
		formAction += " " + formActionExtra
	}
	return "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action " + formAction + "; object-src 'none'"
}

// originOf returns scheme://host of a URL, or "" when it has none.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// securityHeaders sets the response headers. HSTS is only sent when cookies are
// secure, which means the panel is served over HTTPS.
func securityHeaders(hsts bool, formActionExtra string, next http.Handler) http.Handler {
	policy := contentSecurityPolicy(formActionExtra)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Content-Security-Policy", policy)
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginGuard refuses cross-origin mutating requests (CSRF). Requests
// that carry an API key and no session cookie are exempt.
func sameOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mutating(r) && !sameOrigin(r) && !apiKeyOnly(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mutating(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

func apiKeyOnly(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") || r.Header.Get("X-API-Key") == "" {
		return false
	}
	_, err := r.Cookie(auth.CookieName)
	return err != nil
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && u.Host == r.Host
}
