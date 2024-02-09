package importer

import (
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"time"
)

type PinboardBookmark []struct {
	Href        string    `json:"href"`
	Description string    `json:"description"`
	Extended    string    `json:"extended"`
	Meta        string    `json:"meta"`
	Hash        string    `json:"hash"`
	Time        time.Time `json:"time"`
	Shared      string    `json:"shared"`
	Toread      string    `json:"toread"`
	Tags        string    `json:"tags"`
}

var HtmlTagRegex = regexp.MustCompile(`(?s)<[^>]*>(?s)`)

// A go function to remove html tags from a string
// https://stackoverflow.com/questions/1732348/regex-match-open-tags-except-xhtml-self-contained-tags/1732454#1732454
func removeHtmlTags(s string) string {
	return HtmlTagRegex.ReplaceAllString(s, "")
}

func ImportBookmarks(store *repository.Store, importBookmarksJsonFile, importBookmarksUsername string) error {
	file, err := os.Open(importBookmarksJsonFile)
	if err != nil {
		return err
	}
	defer file.Close()
	var pinboardBookmarks = make(PinboardBookmark, 0)
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&pinboardBookmarks)
	if err != nil {
		return err
	}
	bookmarks := make([]domain.Bookmark, 0)
	for _, b := range pinboardBookmarks {
		bookmark := domain.Bookmark{
			URL:         b.Href,
			Title:       b.Description,
			Description: removeHtmlTags(b.Extended),
			Tags:        b.Tags,
			Private:     b.Shared == "no",
			Created:     b.Time,
			Updated:     b.Time,
		}
		bookmarks = append(bookmarks, bookmark)
	}
	log.Println("Importing", len(bookmarks), "bookmarks for user", importBookmarksUsername)
	err = store.InitAndVerifyDb()
	if err != nil {
		panic(err)
	}
	userId, err := store.FindOrCreateUser(importBookmarksUsername)
	if err != nil {
		panic(err)
	}
	// now insert all the bookmarks
	err = store.SaveBookmarks(userId, bookmarks)
	return err
}
