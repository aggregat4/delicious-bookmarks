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

	migrateSchema(db)

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

func initMigrationTable(db *sql.DB) error {
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS migrations (sequence_id INTEGER NOT NULL PRIMARY KEY)")
	return err
}

func existsMigrationTable(db *sql.DB) (bool, error) {
	stmt, err := db.Prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='migrations'")

	if err != nil {
		return false, err
	}

	defer stmt.Close()

	rows, err := stmt.Query()

	if err != nil {
		return false, err
	}

	defer rows.Close()

	return rows.Next(), nil
}

func getAppliedMigrations(db *sql.DB) ([]int, error) {
	stmt, err := db.Prepare("SELECT sequence_id FROM migrations")

	if err != nil {
		return nil, err
	}

	defer stmt.Close()

	rows, err := stmt.Query()

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var migrations []int

	for rows.Next() {
		var sequenceId int
		err = rows.Scan(&sequenceId)

		if err != nil {
			return nil, err
		}

		migrations = append(migrations, sequenceId)
	}

	return migrations, nil
}

type Migration struct {
	SequenceId int
	Sql        string
}

var migrations = []Migration{
	{1,
		`
		CREATE TABLE IF NOT EXISTS users (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		password TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS bookmarks (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		url TEXT NOT NULL UNIQUE,
		title TEXT,
		description TEXT,
		tags TEXT,
		private INTEGER NOT NULL,
		created INTEGER NOT NULl,
		updated INTEGER NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id)
		);
		
		CREATE INDEX IF NOT EXISTS bookmarks_created_idx ON bookmarks(created);
		`,
	},
}

func migrateSchema(db *sql.DB) error {
	exists, err := existsMigrationTable(db)
	if err != nil {
		return err
	}
	if !exists {
		err = initMigrationTable(db)
		if err != nil {
			return err
		}
	}
	appliedMigrations, err := getAppliedMigrations(db)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if !contains(appliedMigrations, migration.SequenceId) {
			_, err = db.Exec(migration.Sql)
			if err != nil {
				return err
			}
			_, err = db.Exec("INSERT INTO migrations (sequence_id) VALUES (?)", migration.SequenceId)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func contains(list []int, item int) bool {
	for _, i := range list {
		if i == item {
			return true
		}
	}
	return false
}
