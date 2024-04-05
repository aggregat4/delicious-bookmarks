package domain

import (
	"time"
)

type BookmarkSlice struct {
	Bookmarks   []Bookmark
	HasLeft     bool
	LeftOffset  int64
	HasRight    bool
	RightOffset int64
	SearchQuery string
	CsrfToken   string
	RssFeedUrl  string
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
	Byline                string
	Content               string
	RetrievalTime         time.Time
	ContentType           string
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
	ServerReadTimeoutSeconds         int
	ServerWriteTimeoutSeconds        int
	SessionCookieSecretKey           string
	ServerPort                       int
}

const (
	DirectionLeft  int = 0
	DirectionRight int = 1
)

type ReadLaterBookmark struct {
	Id           uint64
	Url          string
	AttemptCount int
}

type FeedCandidate struct {
	BookmarkId int
	UserId     int
}
