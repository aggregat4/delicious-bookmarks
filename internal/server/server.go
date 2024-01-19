package server

import (
	"database/sql"
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aggregat4/gobookmarks/internal/crawler"
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/schema"
	"aggregat4/gobookmarks/pkg/crypto"
	"aggregat4/gobookmarks/pkg/lang"

	"github.com/google/uuid"
	"github.com/gorilla/feeds"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

//go:embed public/views/*.html
var viewTemplates embed.FS

//go:embed public/images/*.png
var images embed.FS

func RunServer(config domain.Configuration) {
	db, err := schema.InitAndVerifyDb()
	if err != nil {
		panic(err)
	}
	defer db.Close()

	e := echo.New()
	// Set server timeouts based on advice from https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/#1687428081
	e.Server.ReadTimeout = time.Duration(config.ServerReadTimeoutSeconds) * time.Second
	e.Server.WriteTimeout = time.Duration(config.ServerWriteTimeoutSeconds) * time.Second

	t := &Template{
		templates: template.Must(template.New("").Funcs(template.FuncMap{
			"highlight": func(text string) template.HTML {
				return template.HTML(highlight(template.HTMLEscapeString(text)))
			},
		}).ParseFS(viewTemplates, "public/views/*.html")),
	}
	e.Renderer = t

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	sessionCookieSecretKey := config.SessionCookieSecretKey
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(sessionCookieSecretKey))))
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level: 5,
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:csrf_token",
	}))

	e.GET("/login", func(c echo.Context) error { return showLogin(c) })
	e.POST("/login", func(c echo.Context) error { return login(db, c) })
	e.GET("/bookmarks", func(c echo.Context) error { return showBookmarks(db, c, config) })
	e.POST("/bookmarks", func(c echo.Context) error { return addBookmark(db, c) })
	e.GET("/addbookmark", func(c echo.Context) error { return showAddBookmark(db, c) })
	e.POST("/deletebookmark", func(c echo.Context) error { return deleteBookmark(db, c) })
	e.GET("/feeds/:id", func(c echo.Context) error { return showFeed(db, c, config) })
	e.GET("/images/delicious.png", func(c echo.Context) error { return loadDeliciousImage(c) })

	quitChannel := make(chan struct{})
	crawler.RunBookmarkCrawler(quitChannel, db, config)

	port := config.ServerPort
	e.Logger.Fatal(e.Start(":" + strconv.Itoa(port)))
	// NO MORE CODE HERE, IT WILL NOT BE EXECUTED
}

func loadDeliciousImage(c echo.Context) error {
	file, err := images.ReadFile("public/images/delicious.png")
	if err != nil {
		return err
	}
	c.Response().Header().Set("Content-Type", "image/png")
	_, err = c.Response().Write(file)
	if err != nil {
		return err
	}
	return nil
}

func highlight(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "{{mark}}", "<mark>"), "{{endmark}}", "</mark>")
}

// login handles the login page submission, checking the provided credentials against the database.
// If valid it creates a new session with the user ID saved. It will then redirect to either the
// originally requested URL from the redirect parameter, or to /bookmarks if none provided.
func login(db *sql.DB, c echo.Context) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	redirectUrl := "/bookmarks"
	decodedRedirectUrl, err := base64.StdEncoding.DecodeString(c.Param("redirect"))
	if err == nil {
		redirectUrl = string(decodedRedirectUrl)
	}

	rows, err := db.Query("SELECT id, password FROM users WHERE username = ?", username)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		var passwordHash string
		var userid int
		err = rows.Scan(&userid, &passwordHash)

		if err != nil {
			return err
		}

		if crypto.CheckPasswordHash(password, passwordHash) {
			// we have successfully checked the password, create a session cookie and redirect to the bookmarks page
			sess, _ := session.Get("delicious-bookmarks-session", c)
			sess.Values["userid"] = userid
			err = sess.Save(c.Request(), c.Response())
			if err != nil {
				return err
			}
			return c.Redirect(http.StatusFound, redirectUrl)
		}
	}

	return c.Redirect(http.StatusFound, "/login")
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

type LoginPage struct {
	CsrfToken string
}

func showLogin(c echo.Context) error {
	return c.Render(http.StatusOK, "login", LoginPage{CsrfToken: c.Get("csrf").(string)})
}

func withValidSession(c echo.Context, delegate func(userid int) error) error {
	sess, err := session.Get("delicious-bookmarks-session", c)
	originalRequestUrlBase64 := base64.StdEncoding.EncodeToString([]byte(c.Request().URL.String()))
	if err != nil {
		clearSessionCookie(c)
		return c.Redirect(http.StatusFound, "/login?redirect="+originalRequestUrlBase64)
	} else {
		useridraw := sess.Values["userid"]
		if useridraw == nil {
			log.Println("Found a session but no userid")
			return c.Redirect(http.StatusFound, "/login?redirect="+originalRequestUrlBase64)
		}
		sessionUserid := useridraw.(int)
		if sessionUserid == 0 {
			log.Println("Found a session but no userid")
			return c.Redirect(http.StatusFound, "/login?redirect="+originalRequestUrlBase64)
		} else {
			return delegate(sessionUserid)
		}
	}
}

func getLastModifiedDate(db *sql.DB, userid int) (time.Time, error) {
	rows, err := db.Query("SELECT last_update FROM users WHERE id = ?", userid)
	if err != nil {
		return time.Time{}, err
	}
	defer rows.Close()
	if rows.Next() {
		var updated int64
		err = rows.Scan(&updated)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(updated, 0), nil
	}
	return time.Time{}, nil
}

type AddBookmarkPage struct {
	Bookmark  domain.Bookmark
	CsrfToken string
}

const (
	left  int = 0
	right int = 1
)

func showBookmarks(db *sql.DB, c echo.Context, config domain.Configuration) error {
	return withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Render(http.StatusInternalServerError, "bookmarks", nil)
		}
		currentLastModifiedDateTime, err := getLastModifiedDate(db, userid)
		if err != nil {
			return handleError(err)
		}
		if c.Request().Header.Get("If-Modified-Since") == currentLastModifiedDateTime.Format(http.TimeFormat) {
			return c.NoContent(http.StatusNotModified)
		}
		var direction = right
		if c.QueryParam("direction") != "" {
			direction, err = strconv.Atoi(c.QueryParam("direction"))
			if err != nil {
				direction = right
			}
			if direction != 0 && direction != 1 {
				direction = right
			}
		}
		var offset int64
		if direction == left {
			offset = 0
		} else {
			offset = math.MaxInt64
		}
		if c.QueryParam("offset") != "" {
			offset, _ = strconv.ParseInt(c.QueryParam("offset"), 10, 64)
			// ignore error here, we'll just use the default value
		}
		var searchQuery string = c.QueryParam("q")
		bookmarks := []domain.Bookmark{}
		// This is efficient paging as per https://www2.sqlite.org/cvstrac/wiki?p=ScrollingCursor
		// we use an anchor value and reverse the sorting based on what direction we are paging
		// The baseline is a list of bookmarks in descending order of creation date and moving
		// left means seeing newer bookmarks, and moving right means seeing older bookmarks
		var sqlQuery string
		var rows *sql.Rows
		// we are querying for one element more so that we can determine whether we have reached the end of the dataset or not\
		// this then allows us to disable paging in a certain direction
		if searchQuery != "" {
			if direction == right {
				sqlQuery = `
				SELECT b.url, highlight(bookmarks_fts, 1, '{{mark}}', '{{endmark}}'), highlight(bookmarks_fts, 2, '{{mark}}', '{{endmark}}'), highlight(bookmarks_fts, 3, '{{mark}}', '{{endmark}}'), b.private, b.readlater, b.created, b.updated
				FROM bookmarks_fts bfts, bookmarks b
				WHERE bfts.rowid = b.id
				AND b.user_id = ?
				AND b.created < ?
				AND bookmarks_fts MATCH ?
				ORDER BY b.created DESC
				LIMIT ?`
			} else {
				sqlQuery = `
				SELECT b.url, highlight(bookmarks_fts, 1, '{{mark}}', '{{endmark}}'), highlight(bookmarks_fts, 2, '{{mark}}', '{{endmark}}'), highlight(bookmarks_fts, 3, '{{mark}}', '{{endmark}}'), b.private, b.readlater, b.created, b.updated
				FROM bookmarks_fts bfts, bookmarks b
				WHERE bfts.rowid = b.id
				AND b.user_id = ?
				AND b.created > ?
				AND bookmarks_fts MATCH ?
				ORDER BY b.created ASC
				LIMIT ?`
			}
			rows, err = db.Query(sqlQuery, userid, offset, searchQuery, config.BookmarksPageSize+1)
		} else {
			if direction == right {
				sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created < ? ORDER BY created DESC LIMIT ?"
			} else {
				sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created > ? ORDER BY created ASC LIMIT ?"
			}
			rows, err = db.Query(sqlQuery, userid, offset, config.BookmarksPageSize+1)
		}
		if err != nil {
			return handleError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var url, title, description, tags sql.NullString
			var createdInt, updatedInt, private, readlater int64
			err = rows.Scan(&url, &title, &description, &tags, &private, &readlater, &createdInt, &updatedInt)
			if err != nil {
				return handleError(err)
			}
			bookmarks = append(bookmarks, domain.Bookmark{URL: url.String, Title: title.String, Description: description.String, Tags: tags.String, Private: private == 1, Readlater: readlater == 1, Created: time.Unix(createdInt, 0), Updated: time.Unix(updatedInt, 0)})
		}
		moreResultsLeft := len(bookmarks) == (config.BookmarksPageSize + 1)
		if moreResultsLeft {
			bookmarks = bookmarks[:len(bookmarks)-1]
		}
		if direction == left {
			// if we are moving back in the list of bookmarks the query has given us an ascending list of them
			// we need to reverse them to satisfy the invariant of having a descending list of bookmarks
			for i, j := 0, len(bookmarks)-1; i < j; i, j = i+1, j-1 {
				bookmarks[i], bookmarks[j] = bookmarks[j], bookmarks[i]
			}
		}
		var HasLeft = true
		if /*!(direction == right && offset != 0 && len(bookmarks) == config.BookmarksPageSize) && */ offset == math.MaxInt64 || (direction == left && !moreResultsLeft) {
			HasLeft = false
		}
		var LeftOffset int64 = 0
		if len(bookmarks) > 0 {
			LeftOffset = bookmarks[0].Created.Unix()
		}
		var HasRight = true
		if /* !(direction == left && offset != 0 && len(bookmarks) == config.BookmarksPageSize) && */ offset == 0 || (direction == right && !moreResultsLeft) {
			HasRight = false
		}
		var RightOffset int64 = math.MaxInt64
		if len(bookmarks) >= config.BookmarksPageSize {
			RightOffset = bookmarks[config.BookmarksPageSize-1].Created.Unix()
		}

		feedId, err := getOrCreateFeedIdForUser(db, userid)
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
			RssFeedUrl:  config.DeliciousBookmarksBaseUrl + "/feeds/" + feedId})
	})
}

func getOrCreateFeedIdForUser(db *sql.DB, userid int) (string, error) {
	feedId, err := getFeedIdForUser(db, userid)
	if err != nil {
		if err != sql.ErrNoRows {
			return "", err
		}
		feedId = uuid.New().String()
		_, err = db.Exec("UPDATE users SET feed_id = ? WHERE id = ?", feedId, userid)
		if err != nil {
			return "", err
		}
	}
	return feedId, nil
}

func getFeedIdForUser(db *sql.DB, userid int) (string, error) {
	var feedId sql.NullString
	err := db.QueryRow("SELECT feed_id FROM users WHERE id = ?", userid).Scan(&feedId)
	if err != nil {
		return "", err
	}
	if feedId.Valid {
		return feedId.String, nil
	}
	return "", sql.ErrNoRows
}

func showAddBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Render(http.StatusInternalServerError, "addbookmark", nil)
		}
		url := c.QueryParam("url")
		title := c.QueryParam("title")
		description := c.QueryParam("description")
		if url != "" {
			existingBookmark, err := findExistingBookmark(db, url, userid)
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

func deleteBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			// TODO: add error toast or something based on URL parameter in redirect
			return c.Redirect(http.StatusFound, "/bookmarks")
		}
		url := c.FormValue("url")
		if url != "" {
			id, err := findExistingBookmarkId(db, url, userid)
			if err != nil {
				return handleError(err)
			}
			_, err = db.Exec("DELETE FROM bookmarks WHERE id = ?", id)
			if err != nil {
				return handleError(err)
			}
		}
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
}

func findExistingBookmark(db *sql.DB, url string, userid int) (domain.Bookmark, error) {
	handleError := func(err error) error {
		log.Println(err)
		return err
	}
	rows, err := db.Query("SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND url = ?",
		userid, url)
	if err != nil {
		return domain.Bookmark{}, handleError(err)
	}
	defer rows.Close()
	if rows.Next() {
		var dbUrl, dbTitle, dbDescription, dbTags string
		var dbCreated, dbUpdated, dbPrivate, dbReadlater uint64
		err = rows.Scan(&dbUrl, &dbTitle, &dbDescription, &dbTags, &dbPrivate, &dbReadlater, &dbCreated, &dbUpdated)
		if err != nil {
			return domain.Bookmark{}, handleError(err)
		}
		return domain.Bookmark{URL: dbUrl, Title: dbTitle, Description: dbDescription, Tags: dbTags, Private: dbPrivate == 1, Readlater: dbReadlater == 1, Created: time.Unix(int64(dbCreated), 0), Updated: time.Unix(int64(dbUpdated), 0)}, nil
	}
	return domain.Bookmark{}, nil
}

func findExistingBookmarkId(db *sql.DB, url string, userid int) (int64, error) {
	rows, err := db.Query("SELECT id FROM bookmarks WHERE user_id = ? AND url = ?", userid, url)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	if rows.Next() {
		var id int64
		err = rows.Scan(&id)
		if err != nil {
			return -1, err
		}
		return id, nil
	}
	return -1, errors.New("bookmark not found")
}

func addBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(userid int) error {
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
		privateInt := 0
		if private {
			privateInt = 1
		}
		readlater := c.FormValue("readlater") == "on"
		readlaterInt := 0
		if readlater {
			readlaterInt = 1
		}
		// we perform an upsert because the URL may already be stored and we just want to update the other fields
		_, err := db.Exec(`
			INSERT INTO bookmarks (user_id, url, title, description, tags, private, readlater, created, updated) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(url) DO UPDATE SET title = ?, description = ?, tags = ?, private = ?, readlater = ?, updated = ?`,
			userid, url, title, description, tags, privateInt, readlaterInt, time.Now().Unix(), time.Now().Unix(),
			title, description, tags, privateInt, readlaterInt, time.Now().Unix())
		if err != nil {
			return handleError(err)
		}
		_, err = db.Exec("UPDATE users SET last_update = ? WHERE id = ?", time.Now().Unix(), userid)
		if err != nil {
			return handleError(err)
		}
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
}

func showFeed(db *sql.DB, c echo.Context, config domain.Configuration) error {
	feedId := c.Param("id")
	if feedId == "" {
		return c.String(http.StatusBadRequest, "feed id is required")
	}
	userId, err := findUserIdForFeedId(db, feedId)
	if err != nil {
		log.Println(err)
		// this is not entirely correct, we are returning 404 for all errors but we may also
		// get a random database error and we do not cleanly distinguish between those and not finding the I
		return c.String(http.StatusNotFound, "feed with id "+feedId+" not found")
	}
	readLaterBookmarks, err := findReadLaterBookmarksWithContent(db, userId, config.MaxContentDownloadAttempts)
	if err != nil {
		log.Println(err)
		return c.String(http.StatusInternalServerError, "error retrieving read later bookmarks")
	}
	feed := &feeds.Feed{
		Title:       "Delicious Read Later Bookmarks",
		Link:        &feeds.Link{Href: config.DeliciousBookmarksBaseUrl + "/feeds/" + feedId},
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

func findReadLaterBookmarksWithContent(db *sql.DB, userId string, maxDownloadAttempts int) ([]domain.ReadLaterBookmarkWithContent, error) {
	rows, err := db.Query(
		`
		SELECT b.url, rl.retrieval_status, rl.retrieval_time, rl.title, rl.byline, rl.content, rl.content_type
		FROM read_later rl, bookmarks b
		WHERE rl.user_id = ?
		AND rl.bookmark_id = b.id
		AND (rl.retrieval_status = 0 OR rl.retrieval_attempt_count >= ?)		
		`, userId, maxDownloadAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.ReadLaterBookmarkWithContent
	for rows.Next() {
		var url string
		var retrievalStatus int
		var retrievalTimeOrNull sql.NullInt64
		var title, byline, content, contentType sql.NullString
		err = rows.Scan(&url, &retrievalStatus, &retrievalTimeOrNull, &title, &byline, &content, &contentType)
		// fmt.Println(url, retrievalStatus, retrievalTimeOrNull, title, byline)
		if err != nil {
			return nil, err
		}
		var retrievalTime int64
		retrievalTime = 0
		if retrievalTimeOrNull.Valid {
			retrievalTime = retrievalTimeOrNull.Int64
		}
		result = append(result, domain.ReadLaterBookmarkWithContent{
			Url:                   url,
			SuccessfullyRetrieved: retrievalStatus == 0,
			Title:                 title.String,
			Content:               content.String,
			Byline:                byline.String,
			RetrievalTime:         time.Unix(retrievalTime, 0),
			ContentType:           contentType.String,
		})
	}
	return result, nil
}

func findUserIdForFeedId(db *sql.DB, feedId string) (string, error) {
	rows, err := db.Query("SELECT id FROM users WHERE feed_id = ?", feedId)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if rows.Next() {
		var userId string
		err = rows.Scan(&userId)
		if err != nil {
			return "", err
		}
		return userId, nil
	}
	return "", errors.New("feed not found")
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}
