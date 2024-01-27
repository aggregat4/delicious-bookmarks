package repository

import (
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/pkg/migrations"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	db *sql.DB
}

func (store *Store) Close() {
	store.db.Close()
}

func (store *Store) InitAndVerifyDb() error {
	var err error
	store.db, err = sql.Open("sqlite3", "file:bookmarks.sqlite?_foreign_keys=on")
	if err != nil {
		return err
	}
	return migrations.MigrateSchema(store.db, bookmarkMigrations)
}

func (store *Store) InitDatabaseWithUser(initdbUsername string) error {
	err := store.InitAndVerifyDb()
	if err != nil {
		return err
	}

	rows, err := store.db.Query("SELECT id FROM users WHERE username = ?", initdbUsername)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		feedId := uuid.New().String()
		_, err := store.db.Exec("INSERT INTO users (username, last_update, feed_id) VALUES (?, ?, -1, ?)", initdbUsername, feedId)
		if err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) GetLastModifiedDate(userid int) (time.Time, error) {
	rows, err := store.db.Query("SELECT last_update FROM users WHERE id = ?", userid)
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

func (store *Store) GetBookmarks(searchQuery string, direction int, userid int, offset int64, pageSize int) ([]domain.Bookmark, error) {
	bookmarks := make([]domain.Bookmark, 0)
	// This is efficient paging as per https://www2.sqlite.org/cvstrac/wiki?p=ScrollingCursor
	// we use an anchor value and reverse the sorting based on what direction we are paging
	// The baseline is a list of bookmarks in descending order of creation date and moving
	// left means seeing newer bookmarks, and moving right means seeing older bookmarks
	var sqlQuery string
	var rows *sql.Rows
	var err error
	// we are querying for one element more so that we can determine whether we have reached the end of the dataset or not
	// this then allows us to disable paging in a certain direction
	if searchQuery != "" {
		if direction == domain.DirectionRight {
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
		rows, err = store.db.Query(sqlQuery, userid, offset, searchQuery, pageSize+1)
	} else {
		if direction == domain.DirectionRight {
			sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created < ? ORDER BY created DESC LIMIT ?"
		} else {
			sqlQuery = "SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND created > ? ORDER BY created ASC LIMIT ?"
		}
		rows, err = store.db.Query(sqlQuery, userid, offset, pageSize+1)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var url, title, description, tags sql.NullString
		var createdInt, updatedInt, private, readlater int64
		err = rows.Scan(&url, &title, &description, &tags, &private, &readlater, &createdInt, &updatedInt)
		if err != nil {
			return nil, err
		}
		bookmarks = append(bookmarks, domain.Bookmark{URL: url.String, Title: title.String, Description: description.String, Tags: tags.String, Private: private == 1, Readlater: readlater == 1, Created: time.Unix(createdInt, 0), Updated: time.Unix(updatedInt, 0)})
	}
	return bookmarks, nil
}

func (store *Store) GetOrCreateFeedIdForUser(userid int) (string, error) {
	feedId, err := store.getFeedIdForUser(userid)
	if err != nil {
		if err != sql.ErrNoRows {
			return "", err
		}
		feedId = uuid.New().String()
		_, err = store.db.Exec("UPDATE users SET feed_id = ? WHERE id = ?", feedId, userid)
		if err != nil {
			return "", err
		}
	}
	return feedId, nil
}

func (store *Store) getFeedIdForUser(userid int) (string, error) {
	var feedId sql.NullString
	err := store.db.QueryRow("SELECT feed_id FROM users WHERE id = ?", userid).Scan(&feedId)
	if err != nil {
		return "", err
	}
	if feedId.Valid {
		return feedId.String, nil
	}
	return "", sql.ErrNoRows
}

func (store *Store) FindExistingBookmark(url string, userid int) (domain.Bookmark, error) {
	handleError := func(err error) error {
		log.Println(err)
		return err
	}
	rows, err := store.db.Query("SELECT url, title, description, tags, private, readlater, created, updated FROM bookmarks WHERE user_id = ? AND url = ?",
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

func (store *Store) FindExistingBookmarkId(url string, userid int) (int64, error) {
	rows, err := store.db.Query("SELECT id FROM bookmarks WHERE user_id = ? AND url = ?", userid, url)
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

func (store *Store) DeleteBookmark(url string, userid int) error {
	id, err := store.FindExistingBookmarkId(url, userid)
	if err != nil {
		return err
	}
	_, err = store.db.Exec("DELETE FROM bookmarks WHERE id = ?", id)
	return err
}

func (store *Store) AddOrUpdateBookmark(bookmark domain.Bookmark, userid int) error {

	// we perform an upsert because the URL may already be stored and we just want to update the other fields
	_, err := store.db.Exec(`
			INSERT INTO bookmarks (user_id, url, title, description, tags, private, readlater, created, updated) 
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(url) DO UPDATE SET title = ?, description = ?, tags = ?, private = ?, readlater = ?, updated = ?`,
		userid, bookmark.URL, bookmark.Title, bookmark.Description, bookmark.Tags, bookmark.Private, bookmark.Readlater, time.Now().Unix(), time.Now().Unix(),
		bookmark.Title, bookmark.Description, bookmark.Tags, bookmark.Private, bookmark.Readlater, time.Now().Unix())
	if err != nil {
		return err
	}
	_, err = store.db.Exec("UPDATE users SET last_update = ? WHERE id = ?", time.Now().Unix(), userid)
	return err
}

func (store *Store) FindUserIdForFeedId(feedId string) (string, error) {
	rows, err := store.db.Query("SELECT id FROM users WHERE feed_id = ?", feedId)
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

// FindReadLaterBookmarksWithContent queries the database to find read later bookmarks
// for the given user that have content retrieved, limiting results to those
// that succeeded or had at least maxDownloadAttempts. It returns a slice of
// ReadLaterBookmarkWithContent structs containing the bookmark url, content,
// and metadata.
func (store *Store) FindReadLaterBookmarksWithContent(userId string, maxDownloadAttempts int) ([]domain.ReadLaterBookmarkWithContent, error) {
	rows, err := store.db.Query(
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

func (store *Store) PruneFeedCandidates(cutoffDate int64) error {
	_, err := store.db.Exec(
		`
		DELETE FROM read_later
		WHERE bookmark_id IN (
			SELECT b.id
			FROM bookmarks b
			WHERE b.readlater = 1
			AND b.created < ? 	
		)
		`, cutoffDate,
	)
	return err
}

// GetBookmarksToDownload queries the database to find bookmarks that have been
// marked for downloading content but have not succeeded yet, limiting results
// to those that have had less than maxDownloadAttempts. It returns a slice of
// ReadLaterBookmark structs containing the id, url, and current attempt count
// for each matching bookmark.
func (store *Store) GetBookmarksToDownload(maxDownloadAttempts, maxBookmarksToDownload int) ([]domain.ReadLaterBookmark, error) {
	rows, err := store.db.Query(
		`
        SELECT rl.id, b.url, rl.retrieval_attempt_count
        FROM bookmarks b, read_later rl
        WHERE b.id = rl.bookmark_id
		AND rl.content is NULL
        AND rl.retrieval_attempt_count < ?
		LIMIT ?
        `,
		maxDownloadAttempts, maxBookmarksToDownload,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bookmarksToDownload := make([]domain.ReadLaterBookmark, 0)
	for rows.Next() {
		var readLaterId uint64
		var attemptCount int
		var url string
		err := rows.Scan(&readLaterId, &url, &attemptCount)
		if err != nil {
			panic(err)
		}
		bookmarksToDownload = append(bookmarksToDownload, domain.ReadLaterBookmark{
			Id:           readLaterId,
			Url:          url,
			AttemptCount: attemptCount,
		})
	}
	return bookmarksToDownload, nil
}

func (store *Store) MarkBookmarkAsFailedToDownload(bookmarkId uint64, attempts int) error {
	_, err := store.db.Exec(
		`
		UPDATE read_later
		SET retrieval_status = 1, retrieval_attempt_count = ?
		WHERE id = ?
		`,
		attempts, bookmarkId,
	)
	return err
}

// SaveBookmarkContent updates the read_later table with the content, metadata,
// and download status for the given bookmark ID. It sets the retrieval_status
// to 0 to indicate successful download and increments the attempt count.
func (store *Store) SaveBookmarkContent(bookmarkId uint64, downloadedUrl domain.ReadLaterBookmarkWithContent, content string, attempts int) error {
	_, err := store.db.Exec(
		`
		UPDATE read_later
		SET retrieval_status = 0, retrieval_time = ?, title = ?, byline = ?, content = ?, retrieval_attempt_count = ?, content_type = ?
		WHERE id = ?
		`,
		downloadedUrl.RetrievalTime.Unix(), downloadedUrl.Title, downloadedUrl.Byline, content, attempts, downloadedUrl.ContentType, bookmarkId,
	)
	return err
}

// Identifying new feed candidates means finding bookmarks marked with `readlater` in the last X
// months that have not been added to the read_later table yet and adding them.
func (store *Store) FindFeedCandidates(cutoffDate int64) ([]domain.FeedCandidate, error) {
	rows, err := store.db.Query(
		`
		SELECT b.id, b.user_id
		FROM bookmarks b LEFT JOIN read_later rl ON b.id = rl.bookmark_id
		WHERE b.readlater = 1  
		AND b.created > ?  
		AND rl.id IS NULL
		`, cutoffDate,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feedCandidates := make([]domain.FeedCandidate, 0)
	for rows.Next() {
		var bookmarkId, userId int
		err = rows.Scan(&bookmarkId, &userId)
		if err != nil {
			return nil, err
		}
		log.Printf("Adding new candidate with id %d", bookmarkId)
		feedCandidates = append(feedCandidates, domain.FeedCandidate{BookmarkId: bookmarkId, UserId: userId})
	}
	return feedCandidates, nil
}

func (store *Store) SaveFeedCandidate(feedCandidate domain.FeedCandidate) error {
	_, err := store.db.Exec(
		`INSERT INTO read_later (user_id, bookmark_id, retrieval_attempt_count, retrieval_status) VALUES (?, ?, ?, ?)`,
		feedCandidate.UserId, feedCandidate.BookmarkId, 0, 0)
	return err
}

func (store *Store) FindUserId(username string) (int, error) {
	rows, err := store.db.Query("SELECT id FROM users WHERE username = ?", username)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	if !rows.Next() {
		return -1, errors.New("user not found")
	}
	var userid int
	err = rows.Scan(&userid)
	if err != nil {
		return -1, err
	}
	return userid, nil
}

func (store *Store) SaveBookmarks(userId int, bookmarks []domain.Bookmark) error {
	stmt, err := store.db.Prepare("INSERT INTO bookmarks (user_id, url, title, description, tags, private, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	count := 0
	for _, b := range bookmarks {
		private := 0
		if b.Private {
			private = 1
		}
		_, err = stmt.Exec(userId, b.URL, b.Title, b.Description, b.Tags, private, b.Created.Unix(), b.Updated.Unix())
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
