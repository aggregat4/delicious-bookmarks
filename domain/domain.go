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
