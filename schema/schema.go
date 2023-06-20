package schema

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

func InitAndVerifyDb() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite")
	if err != nil {
		return nil, err
	}

	err = MigrateSchema(db)

	return db, err
}

func InitDatabaseWithUser(initdbUsername, initdbPassword string) error {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite")

	if err != nil {
		return err
	}

	defer db.Close()

	MigrateSchema(db)

	rows, err := db.Query("SELECT password FROM users WHERE id = 1 AND username = ?", initdbUsername)
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
		feedId := uuid.New().String()
		_, err := db.Exec("INSERT INTO users (username, password, last_update, feed_id) VALUES (?, ?, -1, ?)", initdbUsername, initdbPassword, feedId)
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
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='migrations'")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), nil
}

func getAppliedMigrations(db *sql.DB) ([]int, error) {
	rows, err := db.Query("SELECT sequence_id FROM migrations")
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
		password TEXT NOT NULL,
		last_update INTEGER NOT NULL
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
	// adding a full text search index to the bookmarks table
	{2,
		`
		CREATE VIRTUAL TABLE IF NOT EXISTS bookmarks_fts USING fts5(url, title, description, tags, content='bookmarks', content_rowid='id');
		CREATE TRIGGER IF NOT EXISTS bookmarks_ai AFTER INSERT ON bookmarks BEGIN
			INSERT INTO bookmarks_fts(rowid, url, title, description, tags) VALUES (new.id, new.url, new.title, new.description, new.tags);
		END;
		CREATE TRIGGER IF NOT EXISTS bookmarks_ad AFTER DELETE ON bookmarks BEGIN
			INSERT INTO bookmarks_fts(bookmarks_fts, rowid, url, title, description, tags) VALUES('delete', old.id, old.url, old.title, old.description, old.tags);
		END;
		CREATE TRIGGER IF NOT EXISTS bookmarks_au AFTER UPDATE ON bookmarks BEGIN
			INSERT INTO bookmarks_fts(bookmarks_fts, rowid, url, title, description, tags) VALUES('delete', old.id, old.url, old.title, old.description, old.tags);
			INSERT INTO bookmarks_fts(rowid, url, title, description, tags) VALUES (new.id, new.url, new.title, new.description, new.tags);
		END;
		-- populate the bookmarks_fts table with the existing bookmarks when the bookmarks_fts table is empty
		INSERT INTO bookmarks_fts(bookmarks_fts) VALUES('rebuild');
		`,
	},
	// Adding the ability to mark bookmarks as "read later" and generate an RSS feed on them
	{3,
		`
		-- 0 = no read later, 1 = read later
		ALTER TABLE bookmarks ADD COLUMN readlater INTEGER NOT NULL DEFAULT 0;

		-- Should be a UUID for generating a unique feed URL that is unauthenticated but unguessable
		ALTER TABLE users ADD COLUMN feed_id TEXT;

		-- Splitting out the read later bookmark contents into a separate table since it will be
		-- a relatively small subset of all bookmarks and we don't want to bloat the bookmarks table
		-- retrieval_status is 0 for no errors, 1 if an error occurred
		CREATE TABLE IF NOT EXISTS read_later (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		bookmark_id INTEGER NOT NULL,
		retrieval_attempt_count INTEGER NOT NULL,
		retrieval_status INTEGER NOT NULL,
		retrieval_time INTEGER,
		title TEXT,
		content TEXT,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(bookmark_id) REFERENCES bookmarks(id) ON DELETE CASCADE
		)
		`,
	},
}

func MigrateSchema(db *sql.DB) error {
	println("Migrating schema")
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
			println("Executing migration ", migration.SequenceId)
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
