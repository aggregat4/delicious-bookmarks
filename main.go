package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/crypto/bcrypt"

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
	flag.Parse()

	if passwordToHash != "" {
		hashAndPrintPassword(passwordToHash)
	} else if initdbPassword != "" && initdbUsername != "" {
		err := initDatabaseWithUser(initdbUsername, initdbPassword)
		if err != nil {
			log.Fatalf("Error initializing database: %s", err)
		}
	} else {
		runServer()
	}
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
		panic(errors.New("BOOKMARKS_SESSION_COOKIE_SECRET_KEY environment variable must be set"))
	}
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(sessionCookieSecretKey))))

	e.GET("/login", func(c echo.Context) error { return showLogin(c) })
	e.POST("/login", func(c echo.Context) error { return login(db, c) })
	e.GET("/bookmarks", func(c echo.Context) error { return showBookmarks(db, c) })
	e.POST("/bookmarks", func(c echo.Context) error { return addBookmark(db, c) })

	e.Logger.Fatal(e.Start(":1323"))
}

func initAndVerifyDb() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite")
	if err != nil {
		return nil, err
	}
	return db, err
}

func initDatabaseWithUser(initdbUsername, initdbPassword string) error {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite")

	if err != nil {
		return err
	}

	defer db.Close()

	const createUserTableSql string = `
  CREATE TABLE IF NOT EXISTS users (
  id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  password TEXT NOT NULL
  );`

	_, err = db.Exec(createUserTableSql)
	if err != nil {
		return err
	}

	const createBookmarksTableSql string = `
  CREATE TABLE IF NOT EXISTS bookmarks (
  id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
  url TEXT NOT NULL,
  title TEXT,
  description TEXT,
  tags TEXT,
  created INTEGER NOT NULl,
	FOREIGN KEY(user_id) REFERENCES users(id)
  );
	
	CREATE UNIQUE INDEX IF NOT EXISTS bookmarks_created_idx ON bookmarks(created);
	`

	_, err = db.Exec(createBookmarksTableSql)

	if err != nil {
		return err
	}

	stmt, err := db.Prepare("SELECT password FROM users WHERE id = 1 AND username = ?")

	if err != nil {
		return err
	}

	defer stmt.Close()

	rows, err := stmt.Query(initdbUsername)

	if err != nil {
		return err
	}

	defer rows.Close()

	if rows.Next() {
		var password string
		err = rows.Scan(&password)

		if err != nil {
			return err
		}

		if initdbPassword != password {
			return errors.New("the database already has this account but with a different password")
		}
	} else {
		stmt, err := db.Prepare("INSERT INTO users (username, password) VALUES (?, ?)")

		if err != nil {
			return err
		}

		defer stmt.Close()

		_, err = stmt.Exec(initdbUsername, initdbPassword)

		if err != nil {
			return err
		}
	}
	return nil
}

func hashAndPrintPassword(passwordToHash string) error {
	hash, err := hashPassword(passwordToHash)
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

		if checkPasswordHash(password, passwordHash) {
			// we have successfully logged in, create a session cookie and redirect to the bookmarks page
			sess, err := session.Get("session", c)
			if err != nil {
				log.Println("Error getting session: ", err)
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

func showLogin(c echo.Context) error {
	return c.Render(http.StatusOK, "login", "")
}

func withValidSession(c echo.Context, delegate func(username string, userid int) error) error {
	sess, err := session.Get("session", c)
	if err != nil {
		return c.Redirect(http.StatusFound, "/login")
	} else {
		sessionUsername := sess.Values["username"].(string)
		sessionUserid := sess.Values["userid"].(int)
		if sessionUsername == "" || sessionUserid == 0 {
			log.Println("Found a session but no username")
			return c.Redirect(http.StatusFound, "/login")
		} else {
			return delegate(sessionUsername, sessionUserid)
		}
	}
}

type bookmark struct {
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
		bookmarks := []bookmark{}
		for rows.Next() {
			var url, title, description, tags string
			var createdInt int64
			err = rows.Scan(&url, &title, &description, &tags, &createdInt)
			if err != nil {
				return handleError(err)
			}
			bookmarks = append(bookmarks, bookmark{url, title, description, tags, time.Unix(createdInt, 0)})
		}
		return c.Render(http.StatusOK, "bookmarks", bookmarks)
	})
}

func addBookmark(db *sql.DB, c echo.Context) error {
	return withValidSession(c, func(username string, userid int) error {
		handleError := func(err error) error {
			log.Println(err)
			return c.Redirect(http.StatusFound, "/bookmarks")
		}
		url := c.FormValue("url")
		if url == "" {
			handleError(errors.New("URL is required"))
		}
		title := c.FormValue("title")
		description := c.FormValue("description")
		tags := c.FormValue("tags")
		stmt, err := db.Prepare("INSERT INTO bookmarks (user_id, url, title, description, tags, created) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			handleError(err)
		}
		defer stmt.Close()
		_, err = stmt.Exec(userid, url, title, description, tags, time.Now().Unix())
		if err != nil {
			handleError(err)
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

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
