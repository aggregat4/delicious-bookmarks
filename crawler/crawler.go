package crawler

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
)

const MAX_CONTENT_DOWNLOAD_ATTEMPTS = 3
const MAX_CONTENT_DOWNLOAD_TIMEOUT_SECONDS = 20
const MAX_CONTENT_DOWNLOAD_SIZE_BYTES = 2 * 1024 * 1024

// const DEFAULT_FEED_CRAWLING_INTERVAL_SECONDS = 5 * 60
const DEFAULT_FEED_CRAWLING_INTERVAL_SECONDS = 5
const DEFAULT_MONTHS_TO_ADD_TO_FEED = 3

func RunBookmarkCrawler(quitChannel <-chan struct{}, db *sql.DB) {
	ticker := time.NewTicker(DEFAULT_FEED_CRAWLING_INTERVAL_SECONDS * time.Second)
	// use a custom http client so we can set a timeout to make sure we don't hang indefinitely on foreign servers
	downloadHttpClient := &http.Client{
		Timeout: MAX_CONTENT_DOWNLOAD_TIMEOUT_SECONDS * time.Second,
	}
	go func() {
		for {
			select {
			case <-ticker.C:
				findNewFeedCandidates(db)
				// TODO: remove read_later entries older than our cutoff so the feed does not grow unbounded
				// pruneFeedCandidates(db)
				downloadFeedCandidates(db, downloadHttpClient)
			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()
}

// Download all the feed candidates that are not downladed yet and where retrieval_attempt_count is
// not more than our threshold.
func downloadFeedCandidates(db *sql.DB, client *http.Client) {
	rows, err := db.Query(
		`
        SELECT rl.id, b.url, retrieval_attempt_count
        FROM bookmarks b, read_later rl
        WHERE b.id = read_later.bookmark_id
		AND retrieval_content is NULL
        AND retrieval_attempt_count < ?
        `,
		MAX_CONTENT_DOWNLOAD_ATTEMPTS,
	)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var readLaterId, attemptCount uint64
		var url string
		err := rows.Scan(&readLaterId, &url, &attemptCount)
		if err != nil {
			panic(err)
		}

		content, err := downloadContent(url, client)

		if err != nil {
			_, err = db.Exec(
				`
				UPDATE read_later
				SET retrieval_status = 1, retrieval_attempt_count = ?
				WHERE id = ?
				`,
				attemptCount+1, readLaterId,
			)
			if err != nil {
				panic(err)
			}
		} else {
			_, err = db.Exec(
				`
				UPDATE read_later
				SET retrieval_status = 0, retrieval_content = ?, retrieval_attempt_count = ?
				WHERE id = ?
				`,
				content, attemptCount+1, readLaterId,
			)
			if err != nil {
				panic(err)
			}
		}
	}
}

func downloadContent(urlString string, client *http.Client) (string, error) {
	log.Printf("Downloading content for url %s", urlString)

	req, err := http.NewRequest("GET", urlString, nil)
	if err != nil {
		panic(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	limitReader := io.LimitReader(resp.Body, MAX_CONTENT_DOWNLOAD_SIZE_BYTES)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return "", fmt.Errorf("error reading response body from %s: %w", urlString, err)
	}

	content := string(bodyBytes)

	realUrl, err := url.Parse(urlString)
	if err != nil {
		article, err := readability.FromReader(strings.NewReader(content), realUrl)
		if err != nil {
			return "", fmt.Errorf("error parsing content from %s: %w", urlString, err)
		} else {
			return article.Content, nil
		}
	} else {
		return "", err
	}
}

// Identifying new feed candidates means finding bookmarks marked with `readlater` in the last X
// months that have not been added to the read_later table yet and adding them.
func findNewFeedCandidates(db *sql.DB) {
	log.Printf("Finding new feed candidates")
	rows, err := db.Query(
		`
		SELECT b.id, b.user_id
		FROM bookmarks b LEFT JOIN read_later rl ON b.id = rl.bookmark_id
		WHERE b.readlater = 1 
		AND b.created > ? 
		AND rl.id IS NULL
		`,
		time.Now().AddDate(0, -DEFAULT_MONTHS_TO_ADD_TO_FEED, 0))
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var bookmarkId, userId int
		err = rows.Scan(&bookmarkId, &userId)
		if err != nil {
			panic(err)
		}
		log.Printf("Adding new candidate with id %d", bookmarkId)

		err = addFeedCandidate(db, bookmarkId, userId)
		if err != nil {
			panic(err)
		}
	}
	err = rows.Err()
	if err != nil {
		panic(err)
	}
}

func addFeedCandidate(db *sql.DB, bookmarkId int, userId int) error {
	_, err := db.Exec(
		`INSERT INTO read_later (user_id, bookmark_id, retrieval_attempt_count, retrieval_status) VALUES (?, ?, ?, ?)`,
		userId, bookmarkId, 0, 0)
	return err
}
