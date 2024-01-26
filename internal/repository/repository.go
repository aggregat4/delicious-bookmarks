package repository

import (
	"aggregat4/gobookmarks/pkg/migrations"
	"database/sql"

	"github.com/google/uuid"
)

func InitAndVerifyDb() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "file:bookmarks.sqlite?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	migrations.MigrateSchema(db, bookmarkMigrations)

	return db, err
}

func InitDatabaseWithUser(initdbUsername string) (*sql.DB, error) {
	db, err := InitAndVerifyDb()
	if err != nil {
		return nil, err
	}

	rows, err := db.Query("SELECT id FROM users WHERE username = ?", initdbUsername)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		feedId := uuid.New().String()
		_, err := db.Exec("INSERT INTO users (username, last_update, feed_id) VALUES (?, ?, -1, ?)", initdbUsername, feedId)
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}
