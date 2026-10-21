package auth

import (
	"encoding/json"
	"fmt"
)

// LoginProvider is one SSO button. Hint is sent as kc_idp_hint; empty means
// the identity provider's own login page.
type LoginProvider struct {
	Name string `json:"name"`
	Hint string `json:"hint"`
	Icon string `json:"icon,omitempty"`
}

func parseLoginProviders(s string) ([]LoginProvider, error) {
	if s == "" {
		return nil, nil
	}
	var out []LoginProvider
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("login providers: %w", err)
	}
	return out, nil
}

// hintFor returns idp only if a configured provider uses it as hint.
func hintFor(providers []LoginProvider, idp string) string {
	if idp == "" {
		return ""
	}
	for _, p := range providers {
		if p.Hint == idp {
			return p.Hint
		}
	}
	return ""
}
