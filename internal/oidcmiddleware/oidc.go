package oidcmiddleware

import (
	"context"
	"encoding/base64"
	"github.com/aggregat4/go-baselib/crypto"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"
	"log"
	"net/http"
	"time"
)

type OidcMiddleware struct {
	IdpServerUrl string
	ClientId     string
	ClientSecret string
	RedirectUrl  string
	oidcProvider *oidc.Provider
	oidcConfig   oauth2.Config
}

func NewOidcMiddleware(idpServerUrl string, clientId string, clientSecret string, redirectUrl string) *OidcMiddleware {
	ctx := context.Background()
	createdOidcProvider, err := oidc.NewProvider(ctx, idpServerUrl)
	if err != nil {
		panic(err)
	}
	return &OidcMiddleware{
		IdpServerUrl: idpServerUrl,
		ClientId:     clientId,
		ClientSecret: clientSecret,
		RedirectUrl:  redirectUrl,
		oidcProvider: createdOidcProvider,
		oidcConfig: oauth2.Config{
			ClientID:     clientId,
			ClientSecret: clientSecret,
			RedirectURL:  redirectUrl,
			Endpoint:     createdOidcProvider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID},
		},
	}
}

func (oidcMiddleware *OidcMiddleware) CreateOidcMiddleware(isAuthenticated func(c echo.Context) bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !isAuthenticated(c) {
				state, err := crypto.RandString(16)
				if err != nil {
					return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
				}
				// encode the original request URL into the state so we can redirect back to it after a successful login
				// TODO: think about whether storing the original URL like this is generic or should be some sort of custom config
				state = state + "|" + base64.StdEncoding.EncodeToString([]byte(c.Request().URL.String()))
				c.SetCookie(&http.Cookie{
					Name:     "oidc-callback-state-cookie",
					Value:    state,
					Path:     "/", // TODO: this path is not context path safe
					Expires:  time.Now().Add(time.Minute * 5),
					HttpOnly: true,
				})
				return c.Redirect(http.StatusFound, oidcMiddleware.oidcConfig.AuthCodeURL(state))
			} else {
				return next(c)
			}
		}
	}
}

func (oidcMiddleware *OidcMiddleware) CreateOidcCallbackEndpoint(delegate func(c echo.Context, idToken *oidc.IDToken, state string) error) echo.HandlerFunc {
	verifier := oidcMiddleware.oidcProvider.Verifier(&oidc.Config{ClientID: oidcMiddleware.oidcConfig.ClientID})
	return func(c echo.Context) error {
		// check state vs cookie
		state, err := c.Cookie("oidc-callback-state-cookie")
		if err != nil {
			log.Println(err)
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		if c.QueryParam("state") != state.Value {
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		oauth2Token, err := oidcMiddleware.oidcConfig.Exchange(c.Request().Context(), c.QueryParam("code"))
		if err != nil {
			log.Println(err)
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		idToken, err := verifier.Verify(c.Request().Context(), rawIDToken)
		if err != nil {
			log.Println(err)
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		return delegate(c, idToken, state.Value)
	}
}
