package repository

import "aggregat4/gobookmarks/pkg/migrations"

var bookmarkMigrations = []migrations.Migration{
	{SequenceId: 1,
		Sql: `
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
	{SequenceId: 2,
		Sql: `
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
	{SequenceId: 3,
		Sql: `
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
	{SequenceId: 4,
		Sql: `
		-- Adding a column to the read_later table to store the byline of the article
		ALTER TABLE read_later ADD COLUMN byline TEXT;
		ALTER TABLE read_later ADD COLUMN content_type TEXT;
		`,
	},
	{SequenceId: 5,
		Sql: `
		-- Enable WAL mode on the database to allow for concurrent reads and writes
		PRAGMA journal_mode=WAL;
		`,
	},
	{SequenceId: 6,
		Sql: `
		-- Removing the password column from users as we are switching to using openidconnect for authentication
		ALTER TABLE users DROP COLUMN password;
		`,
	},
}
