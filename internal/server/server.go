package server

import (
	"embed"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"

	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"

	baselibmiddleware "github.com/aggregat4/go-baselib-services/v3/middleware"
	baseliboidc "github.com/aggregat4/go-baselib-services/v3/oidc"
	"github.com/aggregat4/go-baselib/lang"

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
	Store  *repository.Store
	Config domain.Configuration
}

func getUserIdFromSession(c echo.Context) (int, error) {
	session, err := session.Get("user_session", c)
	if err != nil {
		return -1, err
	}
	return session.Values["user_id"].(int), nil
}

func RunServer(controller Controller, oidcMiddleware *baseliboidc.OidcMiddleware) {
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
	e.Use(baselibmiddleware.CreateCsrfMiddlewareWithSkipper(func(c echo.Context) bool {
		return false
	}))
	e.Use(oidcMiddleware.CreateOidcMiddleware(
		func(c echo.Context) bool {
			session, err := session.Get("user_session", c)
			if err != nil {
				log.Printf("Error getting session in auth middleware: %v", err)
				return false
			}
			return session.Values["user_id"] != nil

		},
	))
	// Endpoints
	imageFS := echo.MustSubFS(images, "public/images") // MustSubFS basically strips the prefix from the path that is automatically added by Go's embedFS
	e.StaticFS("/images", imageFS)
	e.GET("/oidccallback", oidcMiddleware.CreateOidcCallbackEndpoint(baseliboidc.CreateSessionBasedOidcDelegate(
		func(c echo.Context, idToken *oidc.IDToken) error {
			session, err := session.Get("user_session", c)
			if err != nil {
				log.Printf("Error getting session in auth middleware: %v", err)
				return c.Render(http.StatusInternalServerError, "error-internalserver", nil)
			}
			userId, err := controller.Store.FindOrCreateUser(idToken.Subject)
			if err != nil {
				log.Printf("Error finding or creating user in auth middleware: %v", err)
				return c.Render(http.StatusInternalServerError, "error-internalserver", nil)
			}
			session.Values["user_id"] = userId
			session.Save(c.Request(), c.Response())
			return c.Redirect(http.StatusFound, "/bookmarks")
		},
		"/bookmarks")))
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
	e.GET("/bookmarks", controller.showBookmarks)
	e.POST("/bookmarks", controller.addBookmark)
	e.GET("/addbookmark", controller.showAddBookmark)
	e.POST("/deletebookmark", controller.deleteBookmark)
	e.GET("/feeds/:id", controller.showFeed)
	// Start the server
	port := controller.Config.ServerPort
	e.Logger.Fatal(e.Start(":" + strconv.Itoa(port)))
	// NO MORE CODE HERE, IT WILL NOT BE EXECUTED
}

func handleInternalServerError(c echo.Context, err error) error {
	log.Println(err)
	return c.Render(http.StatusInternalServerError, "error-internalserver", nil)
}

func highlight(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "{{mark}}", "<mark>"), "{{endmark}}", "</mark>")
}

type AddBookmarkPage struct {
	Bookmark domain.Bookmark
}

// TODO: continue here refactoring controller methods into this struct and moving db operations to the repository
func (controller *Controller) showBookmarks(c echo.Context) error {
	userid, err := getUserIdFromSession(c)
	if err != nil {
		return handleInternalServerError(c, err)
	}
	currentLastModifiedDateTime, err := controller.Store.GetLastModifiedDate(userid)
	if err != nil {
		return handleInternalServerError(c, err)
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
	var searchQuery = c.QueryParam("q")
	bookmarks, err := controller.Store.GetBookmarks(searchQuery, direction, userid, offset, controller.Config.BookmarksPageSize)
	if err != nil {
		return handleInternalServerError(c, err)
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
		return handleInternalServerError(c, err)
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
		RssFeedUrl:  controller.Config.DeliciousBookmarksBaseUrl + "/feeds/" + feedId})
}

func (controller *Controller) showAddBookmark(c echo.Context) error {
	handleError := func(err error) error {
		log.Println(err)
		return c.Render(http.StatusInternalServerError, "error-internalserver", nil)
	}
	userid, err := getUserIdFromSession(c)
	if err != nil {
		return handleError(err)
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
			return c.Render(http.StatusOK, "addbookmark", AddBookmarkPage{Bookmark: existingBookmark})
		}
	}
	return c.Render(http.StatusOK, "addbookmark", AddBookmarkPage{Bookmark: domain.Bookmark{URL: url, Title: title, Description: description}})
}

func (controller *Controller) deleteBookmark(c echo.Context) error {
	userid, err := getUserIdFromSession(c)
	if err != nil {
		return handleInternalServerError(c, err)
	}
	url := c.FormValue("url")
	if url != "" {
		err := controller.Store.DeleteBookmark(url, userid)
		if err != nil {
			return handleInternalServerError(c, err)
		}
	}
	return c.Redirect(http.StatusFound, "/bookmarks")
}

func (controller *Controller) addBookmark(c echo.Context) error {
	userid, err := getUserIdFromSession(c)
	if err != nil {
		return handleInternalServerError(c, err)
	}
	url := c.FormValue("url")
	if url == "" {
		return c.Render(http.StatusBadRequest, "error-badrequest", "url parameter is required")
	}
	title := c.FormValue("title")
	description := c.FormValue("description")
	tags := c.FormValue("tags")
	private := c.FormValue("private") == "on"
	readlater := c.FormValue("readlater") == "on"
	err = controller.Store.AddOrUpdateBookmark(domain.Bookmark{URL: url, Title: title, Description: description, Tags: tags, Private: private, Readlater: readlater}, userid)
	if err != nil {
		return handleInternalServerError(c, err)
	}
	return c.Redirect(http.StatusFound, "/bookmarks")
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
		return handleInternalServerError(c, err)
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
		return handleInternalServerError(c, err)
	}
	c.Response().Header().Set("Content-Type", "application/rss+xml")
	return c.String(http.StatusOK, rss)
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, _ echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
