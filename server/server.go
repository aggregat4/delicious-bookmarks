package server

import (
	"database/sql"
	"embed"
	"errors"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"aggregat4/gobookmarks/crawler"
	"aggregat4/gobookmarks/crypto"
	"aggregat4/gobookmarks/domain"
	"aggregat4/gobookmarks/schema"

	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

//go:embed public/views/*.html
var viewTemplates embed.FS

func GetOrDefault(value string, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

func RunServer() {
	db, err := schema.InitAndVerifyDb()
	if err != nil {
		panic(err)
	}
	defer db.Close()

	e := echo.New()

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
	sessionCookieSecretKey := GetOrDefault(os.Getenv("BOOKMARKS_SESSION_COOKIE_SECRET_KEY"), uuid.New().String())
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(sessionCookieSecretKey))))
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level: 5,
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:csrf_token",
	}))

	e.GET("/login", func(c echo.Context) error { return showLogin(c) })
	e.POST("/login", func(c echo.Context) error { return login(db, c) })
	e.GET("/bookmarks", func(c echo.Context) error { return showBookmarks(db, c) })
	e.POST("/bookmarks", func(c echo.Context) error { return addBookmark(db, c) })
	e.GET("/addbookmark", func(c echo.Context) error { return showAddBookmark(db, c) })
	e.POST("/deletebookmark", func(c echo.Context) error { return deleteBookmark(db, c) })

	port := GetOrDefault(os.Getenv("BOOKMARKS_PORT"), "1323")
	e.Logger.Fatal(e.Start(":" + port))

	quitChannel := make(chan struct{})
	crawler.RunBookmarkCrawler(quitChannel, db)
}

func highlight(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "{{mark}}", "<mark>"), "{{endmark}}", "</mark>")
}

func login(db *sql.DB, c echo.Context) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

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
			// we have successfully logged in, create a session cookie and redirect to the bookmarks page
			sess, err := session.Get("delicious-bookmarks-session", c)
			if err != nil {
				log.Println("Error getting session: ", err)
				clearSessionCookie(c)
				return c.Redirect(http.StatusFound, "/login")
			} else {
				sess.Values["userid"] = userid
				sess.Save(c.Request(), c.Response())
				return c.Redirect(http.StatusFound, "/bookmarks")
			}
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
	if err != nil {
		clearSessionCookie(c)
		return c.Redirect(http.StatusFound, "/login")
	} else {
		useridraw := sess.Values["userid"]
		if useridraw == nil {
			log.Println("Found a session but no userid")
			return c.Redirect(http.StatusFound, "/login")
		}
		sessionUserid := useridraw.(int)
		if sessionUserid == 0 {
			log.Println("Found a session but no userid")
			return c.Redirect(http.StatusFound, "/login")
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

const BOOKMARKS_PAGE_SIZE = 50

func showBookmarks(db *sql.DB, c echo.Context) error {
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
			rows, err = db.Query(sqlQuery, userid, offset, searchQuery, BOOKMARKS_PAGE_SIZE+1)
		} else {
			if direction == right {
				sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created < ? ORDER BY created DESC LIMIT ?"
			} else {
				sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created > ? ORDER BY created ASC LIMIT ?"
			}
			rows, err = db.Query(sqlQuery, userid, offset, BOOKMARKS_PAGE_SIZE+1)
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
		moreResultsLeft := len(bookmarks) == (BOOKMARKS_PAGE_SIZE + 1)
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
		if /*!(direction == right && offset != 0 && len(bookmarks) == BOOKMARKS_PAGE_SIZE) && */ offset == math.MaxInt64 || (direction == left && !moreResultsLeft) {
			HasLeft = false
		}
		var LeftOffset int64 = 0
		if len(bookmarks) > 0 {
			LeftOffset = bookmarks[0].Created.Unix()
		}
		var HasRight = true
		if /* !(direction == left && offset != 0 && len(bookmarks) == BOOKMARKS_PAGE_SIZE) && */ offset == 0 || (direction == right && !moreResultsLeft) {
			HasRight = false
		}
		var RightOffset int64 = math.MaxInt64
		if len(bookmarks) >= BOOKMARKS_PAGE_SIZE {
			RightOffset = bookmarks[BOOKMARKS_PAGE_SIZE-1].Created.Unix()
		}

		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Last-Modified", currentLastModifiedDateTime.Format(http.TimeFormat))
		return c.Render(http.StatusOK, "bookmarks", domain.BookmarkSlice{Bookmarks: bookmarks, HasLeft: HasLeft, LeftOffset: LeftOffset, HasRight: HasRight, RightOffset: RightOffset, SearchQuery: searchQuery, CsrfToken: c.Get("csrf").(string)})
	})
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
			existingBookmark, err := findExistingBookmark(db, url, userid)
			if err != nil {
				return handleError(err)
			}
			if existingBookmark == (domain.Bookmark{}) {
				return handleError(errors.New("bookmark not found"))
			}
			_, err = db.Exec("DELETE FROM bookmarks WHERE user_id = ? AND url = ?", userid, url)
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

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}