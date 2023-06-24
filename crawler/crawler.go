package crawler

import (
	"aggregat4/gobookmarks/domain"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
	"github.com/microcosm-cc/bluemonday"
)

func RunBookmarkCrawler(quitChannel <-chan struct{}, db *sql.DB, config domain.Configuration) {
	ticker := time.NewTicker(time.Duration(config.FeedCrawlingIntervalSeconds) * time.Second)
	log.Println("Starting bookmark crawler")
	// use a custom http client so we can set a timeout to make sure we don't hang indefinitely on foreign servers
	downloadHttpClient := &http.Client{
		Timeout: time.Duration(config.MaxContentDownloadTimeoutSeconds) * time.Second,
	}
	sanitisationPolicy := bluemonday.UGCPolicy()
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Println("Running bookmark crawler")
				findNewFeedCandidates(db, config.MonthsToAddToFeed)
				pruneFeedCandidates(db, config.MonthsToAddToFeed)
				downloadNewReadLaterItems(db, downloadHttpClient, sanitisationPolicy, config)
			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()
}

func pruneFeedCandidates(db *sql.DB, monthsToAddToFeed int) {
	log.Println("Pruning feed candidates")
	_, err := db.Exec(
		`
		DELETE FROM read_later
		WHERE bookmark_id IN (
			SELECT b.id
			FROM bookmarks b
			WHERE b.readlater = 1
			AND b.created < ? 	
		)
		`, calculateFeedCutoffDate(monthsToAddToFeed),
	)
	if err != nil {
		panic(err)
	}
}

type ReadLaterBookmark struct {
	Id           uint64
	Url          string
	AttemptCount int
}

// Download all the bookmarks that are not downladed yet and where retrieval_attempt_count is
// not more than our threshold. We also limit the amount of bookmarks we attempt to download so
// that we download in smaller batches and not overwhelm the system
func downloadNewReadLaterItems(db *sql.DB, client *http.Client, sanitisationPolicy *bluemonday.Policy, config domain.Configuration) {
	rows, err := db.Query(
		`
        SELECT rl.id, b.url, rl.retrieval_attempt_count
        FROM bookmarks b, read_later rl
        WHERE b.id = rl.bookmark_id
		AND rl.content is NULL
        AND rl.retrieval_attempt_count < ?
		LIMIT ?
        `,
		config.MaxContentDownloadAttempts, config.MaxBookmarksToDownload, // MAX_CONTENT_DOWNLOAD_ATTEMPTS, MAX_BOOKMARKS_TO_DOWNLOAD,
	)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	bookmarksToDownload := make([]ReadLaterBookmark, 0)

	for rows.Next() {
		var readLaterId uint64
		var attemptCount int
		var url string
		err := rows.Scan(&readLaterId, &url, &attemptCount)
		if err != nil {
			panic(err)
		}
		bookmarksToDownload = append(bookmarksToDownload, ReadLaterBookmark{
			Id:           readLaterId,
			Url:          url,
			AttemptCount: attemptCount,
		})
	}

	rows.Close()

	for _, bookmark := range bookmarksToDownload {
		downloadedUrl, err := downloadContent(bookmark.Url, client, config.MaxContentDownloadSizeBytes)

		if err != nil {
			// TODO: figure out what go logging semantics are since I need this info to debug
			log.Printf("Error downloading content for url '%s' and marking it as failed to download: %s", bookmark.Url, err)
			_, err = db.Exec(
				`
				UPDATE read_later
				SET retrieval_status = 1, retrieval_attempt_count = ?
				WHERE id = ?
				`,
				bookmark.AttemptCount+1, bookmark.Id,
			)
			if err != nil {
				panic(err)
			}
		} else {
			// Sanitise the content with bluemonday just to be sure and to perhaps have some saner content
			sanitised := sanitisationPolicy.Sanitize(downloadedUrl.Content)
			// log.Println(sanitised)
			_, err = db.Exec(
				`
				UPDATE read_later
				SET retrieval_status = 0, retrieval_time = ?, title = ?, byline = ?, content = ?, retrieval_attempt_count = ?
				WHERE id = ?
				`,
				downloadedUrl.RetrievalTime.Unix(), downloadedUrl.Title, downloadedUrl.Byline, sanitised, bookmark.AttemptCount+1, bookmark.Id,
			)
			if err != nil {
				panic(err)
			}
		}
	}
}

func downloadContent(urlString string, client *http.Client, maxContentDownloadSizeBytes int) (domain.ReadLaterBookmarkWithContent, error) {
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

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		fmt.Println("Content Type:", contentType)
	}

	limitReader := io.LimitReader(resp.Body, int64(maxContentDownloadSizeBytes))
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return domain.ReadLaterBookmarkWithContent{}, fmt.Errorf("error reading response body from %s: %w", urlString, err)
	}

	content := string(bodyBytes)

	realUrl, err := url.Parse(urlString)
	if err == nil {
		article, err := readability.FromReader(strings.NewReader(content), realUrl)
		if err != nil {
			return domain.ReadLaterBookmarkWithContent{}, fmt.Errorf("error parsing content from %s: %w", urlString, err)
		} else {
			// log.Println(article.Content)
			// log.Println(article.TextContent)
			return domain.ReadLaterBookmarkWithContent{
				RetrievalTime: time.Now(),
				Title:         article.Title,
				Byline:        article.Byline,
				Content:       article.Content}, nil
		}
	} else {
		return domain.ReadLaterBookmarkWithContent{}, err
	}
}

type FeedCandidate struct {
	bookmarkId int
	userId     int
}

// Identifying new feed candidates means finding bookmarks marked with `readlater` in the last X
// months that have not been added to the read_later table yet and adding them.
func findNewFeedCandidates(db *sql.DB, defaultMonthsToAddToFeed int) {
	log.Printf("Finding new feed candidates")
	feed_cutoff_date := calculateFeedCutoffDate(defaultMonthsToAddToFeed)
	rows, err := db.Query(
		`
		SELECT b.id, b.user_id
		FROM bookmarks b LEFT JOIN read_later rl ON b.id = rl.bookmark_id
		WHERE b.readlater = 1 
		AND b.created > ? 
		AND rl.id IS NULL
		`, feed_cutoff_date,
	)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	feedCandidates := make([]FeedCandidate, 0)

	for rows.Next() {
		var bookmarkId, userId int
		err = rows.Scan(&bookmarkId, &userId)
		if err != nil {
			panic(err)
		}
		log.Printf("Adding new candidate with id %d", bookmarkId)

		feedCandidates = append(feedCandidates, FeedCandidate{bookmarkId, userId})
	}
	rows.Close()

	for _, feedCandidate := range feedCandidates {
		err = addFeedCandidate(db, feedCandidate.bookmarkId, feedCandidate.userId)
		if err != nil {
			panic(err)
		}
	}
}

func calculateFeedCutoffDate(monthsToAddToFeed int) int64 {
	return time.Now().AddDate(0, -monthsToAddToFeed, 0).Unix()
}

func addFeedCandidate(db *sql.DB, bookmarkId int, userId int) error {
	_, err := db.Exec(
		`INSERT INTO read_later (user_id, bookmark_id, retrieval_attempt_count, retrieval_status) VALUES (?, ?, ?, ?)`,
		userId, bookmarkId, 0, 0)
	return err
}
