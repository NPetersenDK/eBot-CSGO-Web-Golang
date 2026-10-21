package auth

// Keeps the OIDC dependencies in go.mod until WP-C imports them for real.
import (
	_ "github.com/coreos/go-oidc/v3/oidc"
	_ "golang.org/x/oauth2"
)
