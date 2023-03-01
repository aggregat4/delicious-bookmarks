package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

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
  id INTEGER NOT NULL PRIMARY KEY,
  username TEXT NOT NULL,
  password TEXT NOT NULL
  );`

	_, err = db.Exec(createUserTableSql)
	if err != nil {
		return err
	}

	const createBookmarksTableSql string = `
  CREATE TABLE IF NOT EXISTS bookmarks (
  id INTEGER NOT NULL PRIMARY KEY,
  url TEXT NOT NULL,
  title TEXT,
  description TEXT,
  tags TEXT,
  created DATETIME NOT NULl
  );`

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
	panic("unimplemented")
}

func showLogin(c echo.Context) error {
	return c.Render(http.StatusOK, "login", "")
}

func showBookmarks(db *sql.DB, c echo.Context) error {
	return c.Render(http.StatusOK, "bookmarks", "Foobar!")
}

func addBookmark(db *sql.DB, c echo.Context) error {
	return c.String(http.StatusOK, "added")
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
