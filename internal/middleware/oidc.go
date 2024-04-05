package middleware

import (
	"encoding/base64"
	"github.com/aggregat4/go-baselib/crypto"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"
	"log"
	"net/http"
	"time"
)

func CreateOidcMiddleware(isAuthenticated func(c echo.Context) bool, oidcConfig oauth2.Config) echo.MiddlewareFunc {
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
				return c.Redirect(http.StatusFound, oidcConfig.AuthCodeURL(state))
			} else {
				return next(c)
			}
		}
	}
}

func CreateOidcCallbackEndpoint(oidcConfig oauth2.Config, oidcProvider *oidc.Provider, delegate func(c echo.Context, idToken *oidc.IDToken, state string) error) echo.HandlerFunc {
	verifier := oidcProvider.Verifier(&oidc.Config{ClientID: oidcConfig.ClientID})
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
		oauth2Token, err := oidcConfig.Exchange(c.Request().Context(), c.QueryParam("code"))
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
