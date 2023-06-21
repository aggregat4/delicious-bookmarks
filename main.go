package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"aggregat4/gobookmarks/crypto"
	"aggregat4/gobookmarks/domain"
	"aggregat4/gobookmarks/importer"
	"aggregat4/gobookmarks/schema"
	"aggregat4/gobookmarks/server"

	"github.com/joho/godotenv"
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
		err := godotenv.Load()
		if err != nil {
			panic(fmt.Errorf("error loading .env file: %s", err))
		}
		server.RunServer(domain.Configuration{
			MaxContentDownloadAttempts:       getIntFromEnv("MAX_CONTENT_DOWNLOAD_ATTEMPTS", 3),
			MaxContentDownloadTimeoutSeconds: getIntFromEnv("MAX_CONTENT_DOWNLOAD_TIMEOUT_SECONDS", 20),
			MaxContentDownloadSizeBytes:      getIntFromEnv("MAX_CONTENT_DOWNLOAD_SIZE_BYTES", 2*1024*1024),
			MaxBookmarksToDownload:           getIntFromEnv("MAX_BOOKMARKS_TO_DOWNLOAD", 20),
			FeedCrawlingIntervalSeconds:      getIntFromEnv("FEED_CRAWLING_INTERVAL_SECONDS", 5*60),
			MonthsToAddToFeed:                getIntFromEnv("MONTHS_TO_ADD_TO_FEED", 6),
			BookmarksPageSize:                getIntFromEnv("BOOKMARKS_PAGE_SIZE", 50),
			DeliciousBookmarksBaseUrl:        requireStringFromEnv("DELICIOUS_BOOKMARKS_BASE_URL"),
		})
	}
}

func requireStringFromEnv(s string) string {
	value := os.Getenv(s)
	if value == "" {
		panic(fmt.Errorf("env variable %s is required", s))
	}
	return value
}

func getIntFromEnv(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Errorf("error parsing env variable %s: %s", key, err))
	}
	return intValue
}
