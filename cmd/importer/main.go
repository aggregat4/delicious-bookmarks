package main

import (
	"flag"
	"log"

	"aggregat4/gobookmarks/internal/importer"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	var importBookmarksHtmlFile string
	flag.StringVar(&importBookmarksHtmlFile, "importFile", "", "A bookmarks.html file to import in the database")
	var importBookmarksUsername string
	flag.StringVar(&importBookmarksUsername, "importUsername", "", "The username to import the bookmarks for")

	flag.Parse()

	if importBookmarksHtmlFile != "" && importBookmarksUsername != "" {
		err := importer.ImportBookmarks(importBookmarksHtmlFile, importBookmarksUsername)
		if err != nil {
			log.Fatalf("Error importing bookmarks: %s", err)
		}
	} else {
		log.Fatalf("require importFile and importUsername parameters when importing ")
	}
}
