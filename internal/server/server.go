package server

import (
	"embed"
	"encoding/base64"
	"errors"
	"github.com/labstack/echo/v4"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"
	"aggregat4/gobookmarks/pkg/crypto"
	"aggregat4/gobookmarks/pkg/lang"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/feeds"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4/middleware"
)

//go:embed public/views/*.html
var viewTemplates embed.FS

//go:embed public/images/*.png
var images embed.FS

type Controller struct {
	Store        *repository.Store
	Config       domain.Configuration
	OidcProvider *oidc.Provider
}

func RunServer(controller Controller) {
	e := echo.New()
	// Set server timeouts based on advice from https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/#1687428081
	e.Server.ReadTimeout = time.Duration(controller.Config.ServerReadTimeoutSeconds) * time.Second
	e.Server.WriteTimeout = time.Duration(controller.Config.ServerWriteTimeoutSeconds) * time.Second

	funcMap := template.FuncMap{
		"highlight": func(text string) template.HTML {
			return template.HTML(highlight(template.HTMLEscapeString(text)))
		},
	}
	t := &Template{
		templates: template.Must(template.New("").Funcs(funcMap).ParseFS(viewTemplates, "public/views/*.html")),
	}
	e.Renderer = t

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	sessionCookieSecretKey := controller.Config.SessionCookieSecretKey
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(sessionCookieSecretKey))))
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level: 5,
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:csrf_token",
	}))

	// MustSubFS basically strips the prefix from the path that is automatically added by Go's embedFS
	imageFS := echo.MustSubFS(images, "public/images")
	e.StaticFS("/images", imageFS)

	e.GET("/oidccallback", controller.oidcCallback)

	e.GET("/bookmarks", controller.showBookmarks)
	e.POST("/bookmarks", controller.addBookmark)
	e.GET("/addbookmark", controller.showAddBookmark)
	e.POST("/deletebookmark", controller.deleteBookmark)
	e.GET("/feeds/:id", controller.showFeed)

	port := controller.Config.ServerPort
	e.Logger.Fatal(e.Start(":" + strconv.Itoa(port)))
	// NO MORE CODE HERE, IT WILL NOT BE EXECUTED
}

func highlight(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "{{mark}}", "<mark>"), "{{endmark}}", "</mark>")
}

func clearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     "delicious-bookmarks-session",
		Value:    "",
		Path:     "/", // TODO: this path is not context path safe
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
}

func setOidcCallbackCookie(c echo.Context, state string) {
	c.SetCookie(&http.Cookie{
		Name:     "delicious-bookmarks-oidc-callback",
		Value:    state,
		Path:     "/", // TODO: this path is not context path safe
		Expires:  time.Now().Add(time.Minute * 5),
		HttpOnly: true,
	})
}

func (controller *Controller) withValidSession(c echo.Context, delegate func(userid int) error) error {
	sess, err := session.Get("delicious-bookmarks-session", c)
	originalRequestUrlBase64 := base64.StdEncoding.EncodeToString([]byte(c.Request().URL.String()))
	handleUnauthenticated := func() error {
		clearSessionCookie(c)
		state, err := crypto.RandString(16)
		if err != nil {
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		state = state + "|" + originalRequestUrlBase64
		setOidcCallbackCookie(c, state)
		return c.Redirect(http.StatusFound, controller.Config.Oauth2Config.AuthCodeURL(state))
	}
	if err != nil {
		return handleUnauthenticated()
	} else {
		useridraw := sess.Values["userid"]
		if useridraw == nil {
			return handleUnauthenticated()
		}
		sessionUserid := useridraw.(int)
		if sessionUserid == 0 {
			return handleUnauthenticated()
		} else {
			return delegate(sessionUserid)
		}
	}
}

func (controller *Controller) oidcCallback(c echo.Context) error {
	// check state vs cookie
	state, err := c.Cookie("delicious-bookmarks-oidc-callback")
	if err != nil {
		log.Println(err)
		return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
	}
	if c.QueryParam("state") != state.Value {
		return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
	}
	oauth2Token, err := controller.Config.Oauth2Config.Exchange(c.Request().Context(), c.QueryParam("code"))
	if err != nil {
		log.Println(err)
		return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
	}
	// TODO: maybe initialize this verifier beforehand and reuse it here
	verifier := controller.OidcProvider.Verifier(&controller.Config.OidcConfig)
	idToken, err := verifier.Verify(c.Request().Context(), rawIDToken)
	if err != nil {
		log.Println(err)
		return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
	}
	// we now have a valid ID token, to progress in the application we need to map this
	// to an existing user or create a new one on demand
	username := idToken.Subject
	userId, err := controller.Store.FindOrCreateUser(username)
	if err != nil {
		log.Println("Error retrieving or creating user: ", err)
		return c.Render(http.StatusInternalServerError, "error-internal", nil)
	}
	// we have a valid user, we can now create a session and redirect to the original request
	sess, _ := session.Get("delicious-bookmarks-session", c)
	sess.Values["userid"] = userId
	err = sess.Save(c.Request(), c.Response())
	if err != nil {
		log.Println(err)
		return c.Render(http.StatusInternalServerError, "error-internal", nil)
	}
	stateParts := strings.Split(state.Value, "|")
	if len(stateParts) > 1 {
		originalRequestUrlBase64 := stateParts[1]
		decodedOriginalRequestUrl, err := base64.StdEncoding.DecodeString(originalRequestUrlBase64)
		if err != nil {
			log.Println(err)
			return c.Render(http.StatusInternalServerError, "error-internal", nil)
		}
		return c.Redirect(http.StatusFound, string(decodedOriginalRequestUrl))
	} else {
		// this is just for robustness, if the state is valid, but does not contain a redirect URL
		// we just go to the bookmarks page
		return c.Redirect(http.StatusFound, "/bookmarks")
	}
}

type AddBookmarkPage struct {
	Bookmark  domain.Bookmark
	CsrfToken string
}

// TODO: continue here refactoring controller methods into this struct and moving db operations to the repository
func (controller *Controller) showBookmarks(c echo.Context) error {
	return controller.withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Render(http.StatusUnauthorized, "error-unauthorized", nil)
		}
		currentLastModifiedDateTime, err := controller.Store.GetLastModifiedDate(userid)
		if err != nil {
			return handleError(err)
		}
		if c.Request().Header.Get("If-Modified-Since") == currentLastModifiedDateTime.Format(http.TimeFormat) {
			return c.NoContent(http.StatusNotModified)
		}
		var direction = domain.DirectionRight
		if c.QueryParam("direction") != "" {
			direction, err = strconv.Atoi(c.QueryParam("direction"))
			if err != nil {
				direction = domain.DirectionRight
			}
			if direction != 0 && direction != 1 {
				direction = domain.DirectionRight
			}
		}
		var offset int64
		if direction == domain.DirectionLeft {
			offset = 0
		} else {
			offset = math.MaxInt64
		}
		if c.QueryParam("offset") != "" {
			offset, _ = strconv.ParseInt(c.QueryParam("offset"), 10, 64)
			// ignore error here, we'll just use the default value
		}
		var searchQuery string = c.QueryParam("q")
		bookmarks, err := controller.Store.GetBookmarks(searchQuery, direction, userid, offset, controller.Config.BookmarksPageSize)
		if err != nil {
			return handleError(err)
		}
		moreResultsLeft := len(bookmarks) == (controller.Config.BookmarksPageSize + 1)
		if moreResultsLeft {
			bookmarks = bookmarks[:len(bookmarks)-1]
		}
		if direction == domain.DirectionLeft {
			// if we are moving back in the list of bookmarks the query has given us an ascending list of them
			// we need to reverse them to satisfy the invariant of having a descending list of bookmarks
			for i, j := 0, len(bookmarks)-1; i < j; i, j = i+1, j-1 {
				bookmarks[i], bookmarks[j] = bookmarks[j], bookmarks[i]
			}
		}
		var HasLeft = true
		if /*!(direction == right && offset != 0 && len(bookmarks) == config.BookmarksPageSize) && */ offset == math.MaxInt64 || (direction == domain.DirectionLeft && !moreResultsLeft) {
			HasLeft = false
		}
		var LeftOffset int64 = 0
		if len(bookmarks) > 0 {
			LeftOffset = bookmarks[0].Created.Unix()
		}
		var HasRight = true
		if /* !(direction == left && offset != 0 && len(bookmarks) == config.BookmarksPageSize) && */ offset == 0 || (direction == domain.DirectionRight && !moreResultsLeft) {
			HasRight = false
		}
		var RightOffset int64 = math.MaxInt64
		if len(bookmarks) >= controller.Config.BookmarksPageSize {
			RightOffset = bookmarks[controller.Config.BookmarksPageSize-1].Created.Unix()
		}

		feedId, err := controller.Store.GetOrCreateFeedIdForUser(userid)
		if err != nil {
			return handleError(err)
		}

		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Last-Modified", currentLastModifiedDateTime.Format(http.TimeFormat))
		return c.Render(http.StatusOK, "bookmarks", domain.BookmarkSlice{
			Bookmarks:   bookmarks,
			HasLeft:     HasLeft,
			LeftOffset:  LeftOffset,
			HasRight:    HasRight,
			RightOffset: RightOffset,
			SearchQuery: searchQuery,
			CsrfToken:   c.Get("csrf").(string),
			RssFeedUrl:  controller.Config.DeliciousBookmarksBaseUrl + "/feeds/" + feedId})
	})
}

func (controller *Controller) showAddBookmark(c echo.Context) error {
	return controller.withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Render(http.StatusInternalServerError, "addbookmark", nil)
		}
		url := c.QueryParam("url")
		title := c.QueryParam("title")
		description := c.QueryParam("description")
		if url != "" {
			existingBookmark, err := controller.Store.FindExistingBookmark(url, userid)
			if err != nil {
				return handleError(err)
			}
			if existingBookmark != (domain.Bookmark{}) {
				return c.Render(http.StatusOK, "addbookmark", AddBookmarkPage{Bookmark: existingBookmark, CsrfToken: c.Get("csrf").(string)})
			}
		}
		return c.Render(http.StatusOK, "addbookmark", AddBookmarkPage{Bookmark: domain.Bookmark{URL: url, Title: title, Description: description}, CsrfToken: c.Get("csrf").(string)})
	})
}

func (controller *Controller) deleteBookmark(c echo.Context) error {
	return controller.withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			// TODO: add error toast or something based on URL parameter in redirect
			return c.Redirect(http.StatusFound, "/bookmarks")
		}
		url := c.FormValue("url")
		if url != "" {
			err := controller.Store.DeleteBookmark(url, userid)
			if err != nil {
				return handleError(err)
			}
		}
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
}

func (controller *Controller) addBookmark(c echo.Context) error {
	return controller.withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println("addBookmark error: ", err)
			return c.Redirect(http.StatusFound, "/bookmarks")
		}
		url := c.FormValue("url")
		if url == "" {
			return handleError(errors.New("URL is required"))
		}
		title := c.FormValue("title")
		description := c.FormValue("description")
		tags := c.FormValue("tags")
		private := c.FormValue("private") == "on"
		readlater := c.FormValue("readlater") == "on"
		err := controller.Store.AddOrUpdateBookmark(domain.Bookmark{URL: url, Title: title, Description: description, Tags: tags, Private: private, Readlater: readlater}, userid)
		if err != nil {
			return handleError(err)
		}
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
}

func (controller *Controller) showFeed(c echo.Context) error {
	feedId := c.Param("id")
	if feedId == "" {
		return c.String(http.StatusBadRequest, "feed id is required")
	}
	userId, err := controller.Store.FindUserIdForFeedId(feedId)
	if err != nil {
		log.Println(err)
		// this is not entirely correct, we are returning 404 for all errors, but we may
		// also get a random database error, and we do not cleanly distinguish between
		// those and not finding the ID
		return c.String(http.StatusNotFound, "feed with id "+feedId+" not found")
	}
	readLaterBookmarks, err := controller.Store.FindReadLaterBookmarksWithContent(userId, controller.Config.MaxContentDownloadAttempts)
	if err != nil {
		log.Println(err)
		return c.String(http.StatusInternalServerError, "error retrieving read later bookmarks")
	}
	feed := &feeds.Feed{
		Title:       "Delicious Read Later Bookmarks",
		Link:        &feeds.Link{Href: controller.Config.DeliciousBookmarksBaseUrl + "/feeds/" + feedId},
		Description: "RSS feed generated of all your delicious bookmarks marked as read later.",
		Created:     time.Now(),
	}

	for _, readLaterBookmark := range readLaterBookmarks {
		if readLaterBookmark.SuccessfullyRetrieved {
			contentTypeIsHtml := readLaterBookmark.ContentType == "" || strings.Contains(readLaterBookmark.ContentType, "text/html")
			item := &feeds.Item{
				Title:   readLaterBookmark.Title,
				Link:    &feeds.Link{Href: readLaterBookmark.Url},
				Content: lang.IfElse(contentTypeIsHtml, readLaterBookmark.Content, "Content is not HTML."),
				Id:      readLaterBookmark.Url + "#" + strconv.FormatInt(readLaterBookmark.RetrievalTime.Unix(), 10),
				// Description: "This is the first item in my RSS feed",
				Author:  &feeds.Author{Name: readLaterBookmark.Byline},
				Created: readLaterBookmark.RetrievalTime,
			}
			feed.Add(item)
		} else {
			// TODO: implement
		}
	}

	rss, err := feed.ToRss()
	if err != nil {
		log.Fatal(err)
	}
	c.Response().Header().Set("Content-Type", "application/rss+xml")
	return c.String(http.StatusOK, rss)
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
