package crawler

import (
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"
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

type Crawler struct {
	Store  *repository.Store
	Config domain.Configuration
}

func (crawler *Crawler) Run(quitChannel <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(crawler.Config.FeedCrawlingIntervalSeconds) * time.Second)
	log.Println("Starting bookmark crawler")
	// use a custom http client so we can set a timeout to make sure we don't hang indefinitely on foreign servers
	downloadHttpClient := &http.Client{
		Timeout: time.Duration(crawler.Config.MaxContentDownloadTimeoutSeconds) * time.Second,
	}
	sanitisationPolicy := bluemonday.UGCPolicy()
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Println("Running bookmark crawler")
				findNewFeedCandidates(crawler.Store, crawler.Config.MonthsToAddToFeed)
				pruneFeedCandidates(crawler.Store, crawler.Config.MonthsToAddToFeed)
				downloadNewReadLaterItems(crawler.Store, downloadHttpClient, sanitisationPolicy, crawler.Config)
			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()
}

func pruneFeedCandidates(store *repository.Store, monthsToAddToFeed int) {
	log.Println("Pruning feed candidates")
	cutoffDate := calculateFeedCutoffDate(monthsToAddToFeed)
	err := store.PruneFeedCandidates(cutoffDate)
	if err != nil {
		panic(err)
	}
}

// Download all the bookmarks that are not downladed yet and where retrieval_attempt_count is
// not more than our threshold. We also limit the amount of bookmarks we attempt to download so
// that we download in smaller batches and not overwhelm the system
func downloadNewReadLaterItems(store *repository.Store, client *http.Client, sanitisationPolicy *bluemonday.Policy, config domain.Configuration) {
	bookmarksToDownload, err := store.GetBookmarksToDownload(config.MaxContentDownloadAttempts, config.MaxBookmarksToDownload)
	if err != nil {
		panic(err)
	}

	for _, bookmark := range bookmarksToDownload {
		downloadedUrl, err := downloadContent(bookmark.Url, client, config.MaxContentDownloadSizeBytes)
		if err != nil {
			// TODO: figure out what go logging semantics are since I need this info to debug
			log.Printf("Error downloading content for url '%s' and marking it as failed to download: %s", bookmark.Url, err)
			err = store.MarkBookmarkAsFailedToDownload(bookmark.Id, bookmark.AttemptCount+1)
		} else {
			// Sanitise the content with bluemonday just to be sure and to perhaps have some saner content
			sanitised := sanitisationPolicy.Sanitize(downloadedUrl.Content)
			// log.Println(sanitised)
			err = store.SaveBookmarkContent(bookmark.Id, downloadedUrl, sanitised, bookmark.AttemptCount+1)
		}
		if err != nil {
			panic(err)
		}
	}
}

func downloadContent(urlString string, client *http.Client, maxContentDownloadSizeBytes int) (domain.ReadLaterBookmarkWithContent, error) {
	log.Printf("Downloading content for url %s", urlString)

	req, err := http.NewRequest("GET", urlString, nil)
	if err != nil {
		return domain.ReadLaterBookmarkWithContent{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return domain.ReadLaterBookmarkWithContent{}, err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")

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
				Content:       article.Content,
				ContentType:   contentType}, nil
		}
	} else {
		return domain.ReadLaterBookmarkWithContent{}, err
	}
}

func findNewFeedCandidates(store *repository.Store, defaultMonthsToAddToFeed int) {
	log.Printf("Finding new feed candidates")
	feed_cutoff_date := calculateFeedCutoffDate(defaultMonthsToAddToFeed)
	feedCandidates, err := store.FindFeedCandidates(feed_cutoff_date)
	if err != nil {
		panic(err)
	}

	for _, feedCandidate := range feedCandidates {
		err = store.SaveFeedCandidate(feedCandidate)
		if err != nil {
			panic(err)
		}
	}
}

func calculateFeedCutoffDate(monthsToAddToFeed int) int64 {
	return time.Now().AddDate(0, -monthsToAddToFeed, 0).Unix()
}
