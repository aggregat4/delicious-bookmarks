package main

import (
	"aggregat4/gobookmarks/internal/crawler"
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/oidcmiddleware"
	"aggregat4/gobookmarks/internal/repository"
	"aggregat4/gobookmarks/internal/server"
	"fmt"
	"github.com/aggregat4/go-baselib/env"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		panic(fmt.Errorf("error loading .env file: %s", err))
	}
	var dbFilename = env.RequireStringFromEnv("DELBM_DB_FILENAME")
	var store repository.Store
	err = store.InitAndVerifyDb(dbFilename)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	// Initialize Oidc Middleware
	oidcMiddleware := oidcmiddleware.NewOidcMiddleware(
		env.RequireStringFromEnv("DELBM_OIDC_IDP_SERVER"),
		env.RequireStringFromEnv("DELBM_OIDC_CLIENT_ID"),
		env.RequireStringFromEnv("DELBM_OIDC_CLIENT_SECRET"),
		env.RequireStringFromEnv("DELBM_OIDC_REDIRECT_URI"))
	// Get and init config
	config := domain.Configuration{
		MaxContentDownloadAttempts:       env.GetIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_ATTEMPTS", 3),
		MaxContentDownloadTimeoutSeconds: env.GetIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_TIMEOUT_SECONDS", 20),
		MaxContentDownloadSizeBytes:      env.GetIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_SIZE_BYTES", 2*1024*1024),
		MaxBookmarksToDownload:           env.GetIntFromEnv("DELBM_MAX_BOOKMARKS_TO_DOWNLOAD", 20),
		FeedCrawlingIntervalSeconds:      env.GetIntFromEnv("DELBM_FEED_CRAWLING_INTERVAL_SECONDS", 5*60),
		MonthsToAddToFeed:                env.GetIntFromEnv("DELBM_MONTHS_TO_ADD_TO_FEED", 6),
		BookmarksPageSize:                env.GetIntFromEnv("DELBM_PAGE_SIZE", 50),
		DeliciousBookmarksBaseUrl:        env.RequireStringFromEnv("DELBM_BASE_URL"),
		ServerReadTimeoutSeconds:         env.GetIntFromEnv("DELBM_SERVER_READ_TIMEOUT_SECONDS", 5),
		ServerWriteTimeoutSeconds:        env.GetIntFromEnv("DELBM_SERVER_WRITE_TIMEOUT_SECONDS", 10),
		SessionCookieSecretKey:           env.GetStringFromEnv("DELBM_SESSION_COOKIE_SECRET_KEY", uuid.New().String()),
		ServerPort:                       env.GetIntFromEnv("DELBM_SERVER_PORT", 1323),
	}
	// Start the bookMarkCrawler
	quitChannel := make(chan struct{})
	bookMarkCrawler := crawler.Crawler{
		Store:  &store,
		Config: config,
	}
	bookMarkCrawler.Run(quitChannel)
	// Start the server
	server.RunServer(server.Controller{
		Store:  &store,
		Config: config,
	}, oidcMiddleware)
}
