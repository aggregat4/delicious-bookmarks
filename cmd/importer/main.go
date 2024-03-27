package main

import (
	"flag"
	"log"

	"aggregat4/gobookmarks/internal/importer"
	"aggregat4/gobookmarks/internal/repository"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	var importBookmarksHtmlFile string
	flag.StringVar(&importBookmarksHtmlFile, "importFile", "", "A bookmarks.html file to import in the database")
	var importBookmarksUsername string
	flag.StringVar(&importBookmarksUsername, "importUsername", "", "The username to import the bookmarks for")
	var bookmarksDbFilename string
	flag.StringVar(&bookmarksDbFilename, "bookmarksDbFilename", "", "Filename of the database to import into")

	flag.Parse()

	if importBookmarksHtmlFile != "" && importBookmarksUsername != "" {
		var store repository.Store
		err := store.InitAndVerifyDb(bookmarksDbFilename)
		defer store.Close()
		if err != nil {
			log.Fatalf("Error initializing database: %s", err)
		}
		err = importer.ImportBookmarks(&store, importBookmarksHtmlFile, importBookmarksUsername)
		if err != nil {
			log.Fatalf("Error importing bookmarks: %s", err)
		}
	} else {
		log.Fatalf("require importFile and importUsername parameters when importing ")
	}
}
