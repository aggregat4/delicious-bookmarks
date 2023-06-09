package main

import (
	"flag"
	"fmt"
	"log"

	"aggregat4/gobookmarks/crypto"
	"aggregat4/gobookmarks/importer"
	"aggregat4/gobookmarks/schema"
	"aggregat4/gobookmarks/server"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	var initdbPassword string
	flag.StringVar(&initdbPassword, "initdb-pass", "", "Initializes the database with a user with this password, contents must be bcrypt encoded")
	var initdbUsername string
	flag.StringVar(&initdbUsername, "initdb-username", "", "Initializes the database with a user with this username")

	var passwordToHash string
	flag.StringVar(&passwordToHash, "passwordtohash", "", "A password that should be hashed and salted and the output sent to stdout")

	var importBookmarksHtmlFile string
	flag.StringVar(&importBookmarksHtmlFile, "importFile", "", "A bookmarks.html file to import in the database")
	var importBookmarksUsername string
	flag.StringVar(&importBookmarksUsername, "importUsername", "", "The username to import the bookmarks for")

	flag.Parse()

	if passwordToHash != "" {
		hash, err := crypto.HashPassword(passwordToHash)
		if err != nil {
			panic(err)
		}
		fmt.Println(hash)
	} else if initdbPassword != "" && initdbUsername != "" {
		err := schema.InitDatabaseWithUser(initdbUsername, initdbPassword)
		if err != nil {
			log.Fatalf("Error initializing database: %s", err)
		}
	} else if importBookmarksHtmlFile != "" && importBookmarksUsername != "" {
		err := importer.ImportBookmarks(importBookmarksHtmlFile, importBookmarksUsername)
		if err != nil {
			log.Fatalf("Error importing bookmarks: %s", err)
		}
	} else {
		server.RunServer()
	}
}
