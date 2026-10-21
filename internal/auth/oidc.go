package auth

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/NPetersenDK/eBot-CSGO-Web-Golang/internal/config"
)

type oidcClient struct {
	oauth         *oauth2.Config
	verifier      *oidc.IDTokenVerifier
	roles         roleMap
	endSessionURL string
}

type oidcResult struct {
	Session    Session
	RawIDToken string
}

// deniedError carries the claims of a user without a role.
type deniedError struct{ page DeniedPage }

func (*deniedError) Error() string { return "no role assigned" }

func newOIDCClient(ctx context.Context, c config.OIDC) (*oidcClient, error) {
	provider, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var disc struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&disc)
	return &oidcClient{
		oauth: &oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			RedirectURL:  c.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:      provider.Verifier(&oidc.Config{ClientID: c.ClientID}),
		roles:         newRoleMap(c.RolesClaim, c.RoleMap, c.DefaultRole),
		endSessionURL: disc.EndSessionEndpoint,
	}, nil
}

func (o *oidcClient) authCodeURL(state, hint, nonce, verifier string) string {
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if hint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("kc_idp_hint", hint))
	}
	return o.oauth.AuthCodeURL(state, opts...)
}

func (o *oidcClient) logoutURL(rawIDToken, postLogoutRedirect string) string {
	if o.endSessionURL == "" {
		return ""
	}
	q := url.Values{}
	if rawIDToken != "" {
		q.Set("id_token_hint", rawIDToken)
	}
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
		q.Set("client_id", o.oauth.ClientID)
	}
	if len(q) == 0 {
		return o.endSessionURL
	}
	return o.endSessionURL + "?" + q.Encode()
}

func (o *oidcClient) exchange(ctx context.Context, code, nonce, verifier string) (oidcResult, error) {
	if nonce == "" || verifier == "" {
		return oidcResult{}, fmt.Errorf("login flow is missing nonce or PKCE verifier")
	}
	tok, err := o.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return oidcResult{}, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return oidcResult{}, fmt.Errorf("no id_token in response")
	}
	idToken, err := o.verifier.Verify(ctx, rawID)
	if err != nil {
		return oidcResult{}, fmt.Errorf("verify id_token: %w", err)
	}
	if idToken.Nonce != nonce {
		return oidcResult{}, fmt.Errorf("id_token nonce mismatch")
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return oidcResult{}, err
	}
	res, err := o.resolve(idToken.Subject, claims)
	res.RawIDToken = rawID
	return res, err
}

// resolve maps verified id_token claims to a session, or a *deniedError.
func (o *oidcClient) resolve(subject string, claims map[string]any) (oidcResult, error) {
	role := o.roles.resolve(stringSliceClaim(claims, o.roles.Claim))
	if role == roleDeny {
		return oidcResult{}, &deniedError{page: DeniedPage{
			Subject:  subject,
			Username: stringClaim(claims, "preferred_username"),
			Name:     stringClaim(claims, "name"),
			Email:    stringClaim(claims, "email"),
			Claim:    o.roles.Claim,
			Values:   stringSliceClaim(claims, o.roles.Claim),
		}}
	}
	name := stringClaim(claims, "name")
	if name == "" {
		name = stringClaim(claims, "preferred_username")
	}
	if name == "" {
		name = stringClaim(claims, "email")
	}
	if name == "" {
		name = subject
	}
	return oidcResult{Session: Session{Subject: subject, Name: name, Role: role, Source: SourceOIDC}}, nil
}
