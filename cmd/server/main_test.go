package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadyzHidesErrors(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	ok := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("dial tcp 10.1.2.3:3306: refused") }

	rec := httptest.NewRecorder()
	readyz(ok, ok)(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("healthy: %d %q", rec.Code, rec.Body.String())
	}

	for name, h := range map[string]http.HandlerFunc{"database": readyz(fail, ok), "redis": readyz(ok, fail)} {
		logs.Reset()
		rec = httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", "/readyz", nil))
		if rec.Code != http.StatusServiceUnavailable || strings.TrimSpace(rec.Body.String()) != "unavailable" {
			t.Errorf("%s down: %d %q", name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "10.1.2.3:3306") || !strings.Contains(logs.String(), name) {
			t.Errorf("%s: error not logged: %s", name, logs.String())
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	rec := httptest.NewRecorder()
	securityHeaders(true, "", next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"base-uri 'none'", "form-action 'self'", "object-src 'none'", "default-src 'self'", "frame-ancestors 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("csp lacks %q: %s", want, csp)
		}
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("hsts = %q", got)
	}

	rec = httptest.NewRecorder()
	securityHeaders(false, "", next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("hsts sent over insecure cookies: %q", got)
	}
}

func TestCSPAllowsTheIdentityProviderForLogout(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	rec := httptest.NewRecorder()
	securityHeaders(true, originOf("https://sso.example.org/realms/main"), next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://sso.example.org;") {
		t.Errorf("csp = %s", csp)
	}
	if got := originOf(""); got != "" {
		t.Errorf("origin of nothing = %q", got)
	}
	rec = httptest.NewRecorder()
	securityHeaders(true, "", next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self';") {
		t.Errorf("without SSO the form action stays on this site: %s", csp)
	}
}
