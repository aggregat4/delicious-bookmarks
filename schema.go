package main

import (
	"database/sql"
	"errors"
)

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
  url TEXT NOT NULL UNIQUE,
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
