package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/require"
	xoauth2 "golang.org/x/oauth2"

	"github.com/dexidp/dex/server/oauth2"
	"github.com/dexidp/dex/storage"
)

// TestPublicClientPKCEFlow covers the MCP-style public client flow:
//   - the authorization endpoint rejects public clients that do not send a
//     PKCE code_challenge, and
//   - a public client can complete the authorization code flow using PKCE
//     and exchange the code without a client secret.
func TestPublicClientPKCEFlow(t *testing.T) {
	verifier := "test-code-verifier-with-enough-entropy-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	t.Run("without code_challenge is rejected", func(t *testing.T) {
		ctx := t.Context()

		httpServer, s := newTestServer(t, nil)
		defer httpServer.Close()

		p, err := oidc.NewProvider(ctx, httpServer.URL)
		require.NoError(t, err)

		var oauth2Client oauth2Client
		oauth2Client.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.Redirect(w, r, oauth2Client.config.AuthCodeURL("state"), http.StatusSeeOther)
				return
			}

			q := r.URL.Query()
			require.Equal(t, oauth2.InvalidRequest, q.Get("error"), q.Get("error_description"))
			require.Contains(t, q.Get("error_description"), "Public clients must use PKCE")

			w.WriteHeader(http.StatusOK)
		}))
		defer oauth2Client.server.Close()

		redirectURL := oauth2Client.server.URL + "/callback"
		require.NoError(t, s.storage.CreateClient(ctx, storage.Client{
			ID:           "public-client",
			Public:       true,
			RedirectURIs: []string{redirectURL},
		}))

		oauth2Client.config = &xoauth2.Config{
			ClientID:    "public-client",
			Endpoint:    p.Endpoint(),
			Scopes:      []string{oidc.ScopeOpenID, "email"},
			RedirectURL: redirectURL,
		}

		resp, err := http.Get(oauth2Client.server.URL + "/login")
		require.NoError(t, err)
		resp.Body.Close()
	})

	t.Run("with PKCE and no client secret succeeds", func(t *testing.T) {
		ctx := t.Context()

		httpServer, s := newTestServer(t, nil)
		defer httpServer.Close()

		p, err := oidc.NewProvider(ctx, httpServer.URL)
		require.NoError(t, err)

		var oauth2Client oauth2Client
		oauth2Client.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.Redirect(w, r, oauth2Client.config.AuthCodeURL("state",
					xoauth2.SetAuthURLParam("code_challenge", codeChallenge),
					xoauth2.SetAuthURLParam("code_challenge_method", "S256"),
				), http.StatusSeeOther)
				return
			}

			q := r.URL.Query()
			require.Equal(t, "", q.Get("error"), q.Get("error_description"))
			code := q.Get("code")
			require.NotEmpty(t, code)

			// Exchange the code without a client secret: the PKCE challenge
			// binds the code to this request.
			token, err := oauth2Client.config.Exchange(ctx, code,
				xoauth2.SetAuthURLParam("code_verifier", verifier))
			require.NoError(t, err)
			require.NotEmpty(t, token.AccessToken)
			require.NotEmpty(t, token.Extra("id_token"))

			w.WriteHeader(http.StatusOK)
		}))
		defer oauth2Client.server.Close()

		redirectURL := oauth2Client.server.URL + "/callback"
		require.NoError(t, s.storage.CreateClient(ctx, storage.Client{
			ID:           "public-client",
			Public:       true,
			RedirectURIs: []string{redirectURL},
		}))

		oauth2Client.config = &xoauth2.Config{
			ClientID:    "public-client",
			Endpoint:    p.Endpoint(),
			Scopes:      []string{oidc.ScopeOpenID, "email"},
			RedirectURL: redirectURL,
		}

		resp, err := http.Get(oauth2Client.server.URL + "/login")
		require.NoError(t, err)
		resp.Body.Close()
	})
}
