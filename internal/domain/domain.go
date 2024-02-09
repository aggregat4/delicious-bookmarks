package domain

import (
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
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
	Oauth2Config                     oauth2.Config
	OidcConfig                       oidc.Config
}

const (
	DirectionRight int = 0
	DirectionLeft  int = 1
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
