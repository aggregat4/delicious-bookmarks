package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"aggregat4/gobookmarks/internal/crawler"
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"
	"aggregat4/gobookmarks/internal/server"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/oauth2"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		panic(fmt.Errorf("error loading .env file: %s", err))
	}
	var dbFilename = requireStringFromEnv("DELBM_DB_FILENAME")

	var store repository.Store
	err = store.InitAndVerifyDb(dbFilename)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	// Initialize OIDC provider
	ctx := context.Background()
	oidcProvider, err := oidc.NewProvider(ctx, requireStringFromEnv("DELBM_OIDC_IDP_SERVER"))
	if err != nil {
		panic(err)
	}
	oauth2Config := oauth2.Config{
		ClientID:     requireStringFromEnv("DELBM_OIDC_CLIENT_ID"),
		ClientSecret: requireStringFromEnv("DELBM_OIDC_CLIENT_SECRET"),
		RedirectURL:  requireStringFromEnv("DELBM_OIDC_REDIRECT_URL"),
		Endpoint:     oidcProvider.Endpoint(),
		// oauth2.Endpoint{
		// 	AuthURL:  requireStringFromEnv("DELBM_OIDC_IDP_AUTH_ENDPOINT"),
		// 	TokenURL: requireStringFromEnv("DELBM_OIDC_IDP_TOKEN_ENDPOINT"),
		// },
		Scopes: []string{oidc.ScopeOpenID},
	}
	// Get and init config
	config := domain.Configuration{
		MaxContentDownloadAttempts:       getIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_ATTEMPTS", 3),
		MaxContentDownloadTimeoutSeconds: getIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_TIMEOUT_SECONDS", 20),
		MaxContentDownloadSizeBytes:      getIntFromEnv("DELBM_MAX_CONTENT_DOWNLOAD_SIZE_BYTES", 2*1024*1024),
		MaxBookmarksToDownload:           getIntFromEnv("DELBM_MAX_BOOKMARKS_TO_DOWNLOAD", 20),
		FeedCrawlingIntervalSeconds:      getIntFromEnv("DELBM_FEED_CRAWLING_INTERVAL_SECONDS", 5*60),
		MonthsToAddToFeed:                getIntFromEnv("DELBM_MONTHS_TO_ADD_TO_FEED", 6),
		BookmarksPageSize:                getIntFromEnv("DELBM_PAGE_SIZE", 50),
		DeliciousBookmarksBaseUrl:        requireStringFromEnv("DELBM_BASE_URL"),
		ServerReadTimeoutSeconds:         getIntFromEnv("DELBM_SERVER_READ_TIMEOUT_SECONDS", 5),
		ServerWriteTimeoutSeconds:        getIntFromEnv("DELBM_SERVER_WRITE_TIMEOUT_SECONDS", 10),
		SessionCookieSecretKey:           getStringFromEnv("DELBM_SESSION_COOKIE_SECRET_KEY", uuid.New().String()),
		ServerPort:                       getIntFromEnv("DELBM_SERVER_PORT", 1323),
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
	}, oauth2Config, oidcProvider)
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

func getStringFromEnv(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
