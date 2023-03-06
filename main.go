package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"html/template"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	var initdbPassword string
	flag.StringVar(&initdbPassword, "initdb-pass", "", "Initializes the database with a user with this password, contents must be bcrypt encoded")
	var initdbUsername string
	flag.StringVar(&initdbUsername, "initdb-username", "", "Initializes the database with a user with this username")

	var passwordToHash string
	flag.StringVar(&passwordToHash, "passwordtohash", "", "A password that should be hashed and salted and the output sent to stdout")

	var importBookmarksHtmlFile string
	flag.StringVar(&importBookmarksHtmlFile, "importFile", "", "A bookmarks.html file to import in the database")
	var importBookmarksUsername string
	flag.StringVar(&importBookmarksUsername, "importUsername", "", "The username to import the bookmarks for")

	flag.Parse()

	if passwordToHash != "" {
		hashAndPrintPassword(passwordToHash)
	} else if initdbPassword != "" && initdbUsername != "" {
		err := initDatabaseWithUser(initdbUsername, initdbPassword)
		if err != nil {
			log.Fatalf("Error initializing database: %s", err)
		}
	} else if importBookmarksHtmlFile != "" && importBookmarksUsername != "" {
		err := importBookmarks(importBookmarksHtmlFile, importBookmarksUsername)
		if err != nil {
			log.Fatalf("Error importing bookmarks: %s", err)
		}
	} else {
		runServer()
	}
}

type PinboardBookmark []struct {
	Href        string    `json:"href"`
	Description string    `json:"description"`
	Extended    string    `json:"extended"`
	Meta        string    `json:"meta"`
	Hash        string    `json:"hash"`
	Time        time.Time `json:"time"`
	Shared      string    `json:"shared"`
	Toread      string    `json:"toread"`
	Tags        string    `json:"tags"`
}

var HtmlTagRegex = regexp.MustCompile(`(?s)<[^>]*>(?s)`)

// A go function to remove html tags from a string
// https://stackoverflow.com/questions/1732348/regex-match-open-tags-except-xhtml-self-contained-tags/1732454#1732454
func removeHtmlTags(s string) string {
	return HtmlTagRegex.ReplaceAllString(s, "")
}

func importBookmarks(importBookmarksJsonFile, importBookmarksUsername string) error {
	file, err := os.Open(importBookmarksJsonFile)
	if err != nil {
		return err
	}
	defer file.Close()
	var pinboardBookmarks = make(PinboardBookmark, 0)
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&pinboardBookmarks)
	if err != nil {
		return err
	}
	bookmarks := make([]Bookmark, 0)
	for _, b := range pinboardBookmarks {
		bookmark := Bookmark{
			URL:         b.Href,
			Title:       b.Description,
			Description: removeHtmlTags(b.Extended),
			Tags:        b.Tags,
			Created:     b.Time,
		}
		bookmarks = append(bookmarks, bookmark)
	}
	log.Println("Importing", len(bookmarks), "bookmarks for user", importBookmarksUsername)
	// now import all the bookmarks in the database
	db, err := initAndVerifyDb()
	if err != nil {
		panic(err)
	}
	defer db.Close()
	// get the user id
	stmt, err := db.Prepare("SELECT id FROM users WHERE username = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(importBookmarksUsername)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return errors.New("user not found")
	}
	var userid int
	err = rows.Scan(&userid)
	if err != nil {
		return err
	}
	rows.Close()
	stmt.Close()
	// now insert all the bookmarks
	stmt, err = db.Prepare("INSERT INTO bookmarks (user_id, url, title, description, tags, created) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	count := 0
	for _, b := range bookmarks {
		_, err = stmt.Exec(userid, b.URL, b.Title, b.Description, b.Tags, b.Created.Unix())
		if err != nil {
			return err
		}
		count = count + 1
		if count%25 == 0 {
			log.Println("Imported", count, "bookmarks")
		}
	}
	return nil
}

func runServer() {
	db, err := initAndVerifyDb()
	if err != nil {
		panic(err)
	}
	defer db.Close()

	e := echo.New()

	t := &Template{
		templates: template.Must(template.ParseGlob("public/views/*.html")),
	}
	e.Renderer = t

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	sessionCookieSecretKey := os.Getenv("BOOKMARKS_SESSION_COOKIE_SECRET_KEY")
	if sessionCookieSecretKey == "" {
		// generate a cookie secret key
		sessionCookieSecretKey = uuid.New().String()
	}
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(sessionCookieSecretKey))))
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level: 5,
	}))

	e.GET("/login", func(c echo.Context) error { return showLogin(c) })
	e.POST("/login", func(c echo.Context) error { return login(db, c) })
	e.GET("/bookmarks", func(c echo.Context) error { return showBookmarks(db, c) })
	e.POST("/bookmarks", func(c echo.Context) error { return addBookmark(db, c) })
	e.GET("/addbookmark", func(c echo.Context) error { return showAddBookmark(db, c) })

	e.Logger.Fatal(e.Start(":1323"))
}

func initAndVerifyDb() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite")
	if err != nil {
		return nil, err
	}
	return db, err
}

func hashAndPrintPassword(passwordToHash string) error {
	hash, err := HashPassword(passwordToHash)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func login(db *sql.DB, c echo.Context) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	stmt, err := db.Prepare("SELECT id, password FROM users WHERE username = ?")

	if err != nil {
		return err
	}

	defer stmt.Close()

	rows, err := stmt.Query(username)

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

		if CheckPasswordHash(password, passwordHash) {
			// we have successfully logged in, create a session cookie and redirect to the bookmarks page
			sess, err := session.Get("delicious-bookmarks-session", c)
			if err != nil {
				log.Println("Error getting session: ", err)
				clearSessionCookie(c)
				return c.Redirect(http.StatusFound, "/login")
			} else {
				sess.Values["userid"] = userid
				sess.Values["username"] = username
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

func showLogin(c echo.Context) error {
	return c.Render(http.StatusOK, "login", "")
}

func withValidSession(c echo.Context, delegate func(username string, userid int) error) error {
	sess, err := session.Get("delicious-bookmarks-session", c)
	if err != nil {
		clearSessionCookie(c)
		return c.Redirect(http.StatusFound, "/login")
	} else {
		usernameraw := sess.Values["username"]
		useridraw := sess.Values["userid"]
		if usernameraw == nil || useridraw == nil {
			log.Println("Found a session but no username or userid")
			return c.Redirect(http.StatusFound, "/login")
		}
		sessionUsername := usernameraw.(string)
		sessionUserid := useridraw.(int)
		if sessionUsername == "" || sessionUserid == 0 {
			log.Println("Found a session but no username")
			return c.Redirect(http.StatusFound, "/login")
		} else {
			return delegate(sessionUsername, sessionUserid)
		}
	}
}

type Bookmark struct {
	URL         string
	Title       string
	Description string
	Tags        string
	Created     time.Time
}

func showBookmarks(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(username string, userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Render(http.StatusInternalServerError, "bookmarks", nil)
		}
		stmt, err := db.Prepare("SELECT url, title, description, tags, created FROM bookmarks WHERE user_id = ? ORDER BY created DESC")
		if err != nil {
			return handleError(err)
		}
		defer stmt.Close()
		rows, err := stmt.Query(userid)
		if err != nil {
			return handleError(err)
		}
		defer rows.Close()
		bookmarks := []Bookmark{}
		for rows.Next() {
			var url, title, description, tags string
			var createdInt int64
			err = rows.Scan(&url, &title, &description, &tags, &createdInt)
			if err != nil {
				return handleError(err)
			}
			bookmarks = append(bookmarks, Bookmark{url, title, description, tags, time.Unix(createdInt, 0)})
		}
		return c.Render(http.StatusOK, "bookmarks", bookmarks)
	})
}

func showAddBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(username string, userid int) error {
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
			if existingBookmark != (Bookmark{}) {
				return c.Render(http.StatusOK, "addbookmark", existingBookmark)
			}
		}
		return c.Render(http.StatusOK, "addbookmark", Bookmark{URL: url, Title: title, Description: description})
	})
}

func findExistingBookmark(db *sql.DB, url string, userid int) (Bookmark, error) {
	handleError := func(err error) error {
		log.Println(err)
		return err
	}
	stmt, err := db.Prepare("SELECT url, title, description, tags, created FROM bookmarks WHERE user_id = ? AND url = ?")
	if err != nil {
		return Bookmark{}, handleError(err)
	}
	defer stmt.Close()
	rows, err := stmt.Query(userid, url)
	if err != nil {
		return Bookmark{}, handleError(err)
	}
	defer rows.Close()
	if rows.Next() {
		var dbUrl, dbTitle, dbDescription, dbTags string
		var dbCreated uint64
		err = rows.Scan(&dbUrl, &dbTitle, &dbDescription, &dbTags, &dbCreated)
		if err != nil {
			return Bookmark{}, handleError(err)
		}
		return Bookmark{URL: dbUrl, Title: dbTitle, Description: dbDescription, Tags: dbTags, Created: time.Unix(int64(dbCreated), 0)}, nil
	}
	return Bookmark{}, nil
}

func addBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(username string, userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Redirect(http.StatusFound, "/bookmarks")
		}
		url := c.FormValue("url")
		if url == "" {
			return handleError(errors.New("URL is required"))
		}
		title := c.FormValue("title")
		description := c.FormValue("description")
		tags := c.FormValue("tags")
		// we perform an upsert because the URL may already be stored and we just want to update the other fields
		stmt, err := db.Prepare(`
			INSERT INTO bookmarks (user_id, url, title, description, tags, created) 
			VALUES (?, ?, ?, ?, ?, ?) 
			ON CONFLICT(url) DO UPDATE SET title = ?, description = ?, tags = ?`)
		if err != nil {
			return handleError(err)
		}
		defer stmt.Close()
		_, err = stmt.Exec(userid, url, title, description, tags, time.Now().Unix(), title, description, tags)
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
