package domain

import "time"

type BookmarkSlice struct {
	Bookmarks   []Bookmark
	HasLeft     bool
	LeftOffset  int64
	HasRight    bool
	RightOffset int64
	SearchQuery string
	CsrfToken   string
}

type Bookmark struct {
	URL         string
	Title       string
	Description string
	Tags        string
	Private     bool
	Readlater   bool
	Created     time.Time
	Updated     time.Time
}

type ReadLaterBookmarkWithContent struct {
	Url                   string
	SuccessfullyRetrieved bool
	Title                 string
	Content               string
	RetrievalTime         time.Time
}

type Configuration struct {
	MaxContentDownloadAttempts       int
	MaxContentDownloadTimeoutSeconds int
	MaxContentDownloadSizeBytes      int
	MaxBookmarksToDownload           int
	FeedCrawlingIntervalSeconds      int
	MonthsToAddToFeed                int
	BookmarksPageSize                int
	DeliciousBookmarksBaseUrl        string
}
