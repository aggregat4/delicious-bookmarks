package server

import (
	"aggregat4/gobookmarks/internal/domain"
	"aggregat4/gobookmarks/internal/repository"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer represents a test server instance
type TestServer struct {
	Server     *echo.Echo
	Store      *repository.Store
	Config     domain.Configuration
	TestUserID int
	DBPath     string
}

// setupTestServer creates a test server with in-memory database
func setupTestServer(t *testing.T) *TestServer {
	// Create temporary database file
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Initialize store
	store := &repository.Store{}
	err := store.InitAndVerifyDb(dbPath)
	require.NoError(t, err)

	// Create test configuration
	config := domain.Configuration{
		MaxContentDownloadAttempts:       3,
		MaxContentDownloadTimeoutSeconds: 30,
		MaxContentDownloadSizeBytes:      1024 * 1024,
		MaxBookmarksToDownload:           100,
		FeedCrawlingIntervalSeconds:      3600,
		MonthsToAddToFeed:                6,
		BookmarksPageSize:                10,
		DeliciousBookmarksBaseUrl:        "https://api.delicious.com",
		ServerReadTimeoutSeconds:         30,
		ServerWriteTimeoutSeconds:        30,
		SessionCookieSecretKey:           "test-secret-key-32-chars-long",
		ServerPort:                       8080,
	}

	// Create test user
	testUserID, err := store.FindOrCreateUser("test-user")
	require.NoError(t, err)

	// Create Echo server using the same setup as the real server
	e := echo.New()

	// Set server timeouts
	e.Server.ReadTimeout = time.Duration(config.ServerReadTimeoutSeconds) * time.Second
	e.Server.WriteTimeout = time.Duration(config.ServerWriteTimeoutSeconds) * time.Second

	// Set up templates (same as real server)
	funcMap := template.FuncMap{
		"highlight": func(text string) template.HTML {
			return template.HTML(highlight(template.HTMLEscapeString(text)))
		},
	}
	tmpl := &Template{
		templates: template.Must(template.New("").Funcs(funcMap).ParseFS(viewTemplates, "public/views/*.html")),
	}
	e.Renderer = tmpl

	// Set up middleware (same as real server, but without OIDC)
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(session.Middleware(sessions.NewCookieStore([]byte(config.SessionCookieSecretKey))))
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{Level: 5}))

	// Create controller (same as real server)
	controller := &Controller{
		Store:  store,
		Config: config,
	}

	// Set up routes (same as real server)
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/bookmarks")
	})
	e.GET("/bookmarks", controller.showBookmarks)
	e.POST("/bookmarks", controller.addBookmark)
	e.GET("/addbookmark", controller.showAddBookmark)
	e.POST("/deletebookmark", controller.deleteBookmark)
	e.GET("/feeds/:id", controller.showFeed)

	return &TestServer{
		Server:     e,
		Store:      store,
		Config:     config,
		TestUserID: testUserID,
		DBPath:     dbPath,
	}
}

// createTestSession creates a session for the test user
func (ts *TestServer) createTestSession(t *testing.T) string {
	store := sessions.NewCookieStore([]byte(ts.Config.SessionCookieSecretKey))
	session := sessions.NewSession(store, "user_session")
	session.Values["user_id"] = ts.TestUserID

	// Create a request and response to properly set the session cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	err := session.Save(req, rec)
	require.NoError(t, err)

	// Extract the session cookie
	cookies := rec.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "user_session" {
			return cookie.Value
		}
	}
	t.Fatal("Session cookie not found")
	return ""
}

// addTestBookmarks adds sample bookmarks to the database
func (ts *TestServer) addTestBookmarks(t *testing.T) {
	bookmarks := []domain.Bookmark{
		{
			URL:         "https://example.com/article1",
			Title:       "Test Article 1",
			Description: "This is a test article about technology",
			Tags:        "tech,programming",
			Private:     false,
			Readlater:   false,
			Created:     time.Now().Add(-24 * time.Hour),
			Updated:     time.Now().Add(-24 * time.Hour),
		},
		{
			URL:         "https://example.com/article2",
			Title:       "Test Article 2",
			Description: "Another test article about science",
			Tags:        "science,research",
			Private:     true,
			Readlater:   true,
			Created:     time.Now().Add(-48 * time.Hour),
			Updated:     time.Now().Add(-48 * time.Hour),
		},
		{
			URL:         "https://example.com/article3",
			Title:       "Test Article 3",
			Description: "A third test article about programming",
			Tags:        "programming,golang",
			Private:     false,
			Readlater:   false,
			Created:     time.Now().Add(-72 * time.Hour),
			Updated:     time.Now().Add(-72 * time.Hour),
		},
	}

	err := ts.Store.SaveBookmarks(ts.TestUserID, bookmarks)
	require.NoError(t, err)
}

// cleanup cleans up test resources
func (ts *TestServer) cleanup() {
	if ts.Store != nil {
		ts.Store.Close()
	}
	if ts.DBPath != "" {
		os.Remove(ts.DBPath)
	}
}

func TestIntegration_BookmarksPage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add test bookmarks
	ts.addTestBookmarks(t)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/bookmarks", nil)
	rec := httptest.NewRecorder()

	// Create session
	sessionCookie := ts.createTestSession(t)
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Test Article 1<")
	assert.Contains(t, body, "Test Article 2<")
	assert.Contains(t, body, "Test Article 3<")
	assert.Contains(t, body, "https://example.com/article1")
	assert.Contains(t, body, "https://example.com/article2")
	assert.Contains(t, body, "https://example.com/article3")
	assert.Contains(t, body, `<div class="bookmark-url">https://example.com/article1</div>`)
}

func TestIntegration_BookmarksPageWithSearch(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add test bookmarks
	ts.addTestBookmarks(t)

	// Create request with search query
	req := httptest.NewRequest(http.MethodGet, "/bookmarks?q=programming", nil)
	rec := httptest.NewRecorder()

	// Create session
	sessionCookie := ts.createTestSession(t)
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Test Article 1<")    // Contains "programming" in tags
	assert.Contains(t, body, "Test Article 3<")    // Contains "programming" in tags
	assert.NotContains(t, body, "Test Article 2<") // Should not contain "science" article
}

func TestIntegration_AddBookmarkPage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/addbookmark", nil)
	rec := httptest.NewRecorder()

	// Create session
	sessionCookie := ts.createTestSession(t)
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Add Bookmark")
}

func TestIntegration_AddBookmark(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	sessionCookie := ts.createTestSession(t)

	// Create POST request to add bookmark
	formData := url.Values{}
	formData.Set("url", "https://example.com/new-article")
	formData.Set("title", "New Test Article")
	formData.Set("description", "A new test article")
	formData.Set("tags", "test,new")
	formData.Set("private", "false")
	formData.Set("readlater", "on") // HTML checkbox sends "on" when checked

	req := httptest.NewRequest(http.MethodPost, "/bookmarks", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "user_session="+sessionCookie)
	rec := httptest.NewRecorder()

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusFound, rec.Code) // Should redirect

	// Verify bookmark was added to database
	bookmark, err := ts.Store.FindExistingBookmark("https://example.com/new-article", ts.TestUserID)
	require.NoError(t, err)
	assert.Equal(t, "New Test Article", bookmark.Title)
	assert.Equal(t, "A new test article", bookmark.Description)
	assert.Equal(t, "test,new", bookmark.Tags)
	assert.False(t, bookmark.Private)
	assert.True(t, bookmark.Readlater)
}

func TestIntegration_DeleteBookmark(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add test bookmarks
	ts.addTestBookmarks(t)

	sessionCookie := ts.createTestSession(t)

	// Verify bookmark exists before deletion
	_, err := ts.Store.FindExistingBookmarkId("https://example.com/article1", ts.TestUserID)
	require.NoError(t, err)

	// Create POST request to delete bookmark
	formData := url.Values{}
	formData.Set("url", "https://example.com/article1") // Delete endpoint expects URL, not bookmark_id

	req := httptest.NewRequest(http.MethodPost, "/deletebookmark", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "user_session="+sessionCookie)
	rec := httptest.NewRecorder()

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusFound, rec.Code) // Should redirect

	// Verify bookmark was deleted from database
	_, err = ts.Store.FindExistingBookmarkId("https://example.com/article1", ts.TestUserID)
	assert.Error(t, err) // Should not find the bookmark
}

func TestIntegration_RSSFeed(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add test bookmarks with readlater flag
	bookmarks := []domain.Bookmark{
		{
			URL:         "https://example.com/article1",
			Title:       "Test Article 1",
			Description: "This is a test article about technology",
			Tags:        "tech,programming",
			Private:     false,
			Readlater:   true, // Mark as read later
			Created:     time.Now().Add(-24 * time.Hour),
			Updated:     time.Now().Add(-24 * time.Hour),
		},
		{
			URL:         "https://example.com/article2",
			Title:       "Test Article 2",
			Description: "Another test article about science",
			Tags:        "science,research",
			Private:     true,
			Readlater:   true, // Mark as read later
			Created:     time.Now().Add(-48 * time.Hour),
			Updated:     time.Now().Add(-48 * time.Hour),
		},
	}

	err := ts.Store.SaveBookmarks(ts.TestUserID, bookmarks)
	require.NoError(t, err)

	// Get bookmark IDs and add them to read_later table with content
	bookmark1ID, err := ts.Store.FindExistingBookmarkId("https://example.com/article1", ts.TestUserID)
	require.NoError(t, err)

	bookmark2ID, err := ts.Store.FindExistingBookmarkId("https://example.com/article2", ts.TestUserID)
	require.NoError(t, err)

	// Add entries to read_later table with content
	// First save feed candidates
	err = ts.Store.SaveFeedCandidate(domain.FeedCandidate{BookmarkId: int(bookmark1ID), UserId: ts.TestUserID})
	require.NoError(t, err)
	err = ts.Store.SaveFeedCandidate(domain.FeedCandidate{BookmarkId: int(bookmark2ID), UserId: ts.TestUserID})
	require.NoError(t, err)

	// Then update with content (this would normally be done by the crawler)
	// For testing, we'll use the SaveBookmarkContent method
	content1 := domain.ReadLaterBookmarkWithContent{
		Url:                   "https://example.com/article1",
		SuccessfullyRetrieved: true,
		Title:                 "Test Article 1",
		Byline:                "Test Author",
		Content:               "<html><body>Test content 1</body></html>",
		RetrievalTime:         time.Now(),
		ContentType:           "text/html",
	}

	content2 := domain.ReadLaterBookmarkWithContent{
		Url:                   "https://example.com/article2",
		SuccessfullyRetrieved: true,
		Title:                 "Test Article 2",
		Byline:                "Test Author",
		Content:               "<html><body>Test content 2</body></html>",
		RetrievalTime:         time.Now(),
		ContentType:           "text/html",
	}

	// Get the read_later IDs (they should be 1 and 2 since we just inserted them)
	err = ts.Store.SaveBookmarkContent(1, content1, content1.Content, 1)
	require.NoError(t, err)
	err = ts.Store.SaveBookmarkContent(2, content2, content2.Content, 1)
	require.NoError(t, err)

	// Get feed ID for user
	feedID, err := ts.Store.GetOrCreateFeedIdForUser(ts.TestUserID)
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/feeds/"+feedID, nil)
	rec := httptest.NewRecorder()

	// Execute request
	ts.Server.ServeHTTP(rec, req)

	// Assertions
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<?xml")
	assert.Contains(t, body, "<rss")
	assert.Contains(t, body, "Test Article 1")
	assert.Contains(t, body, "Test Article 2")
	assert.Contains(t, body, "https://example.com/article1")
	assert.Contains(t, body, "https://example.com/article2")
}

func TestIntegration_Pagination(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add more bookmarks than page size
	bookmarks := make([]domain.Bookmark, 15)
	for i := 0; i < 15; i++ {
		bookmarks[i] = domain.Bookmark{
			URL:         fmt.Sprintf("https://example.com/article%d", i+1),
			Title:       fmt.Sprintf("Test Article %d", i+1),
			Description: fmt.Sprintf("Description for article %d", i+1),
			Tags:        "test",
			Private:     false,
			Readlater:   false,
			Created:     time.Now().Add(-time.Duration(i) * time.Hour),
			Updated:     time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}

	err := ts.Store.SaveBookmarks(ts.TestUserID, bookmarks)
	require.NoError(t, err)

	// Test first page
	req := httptest.NewRequest(http.MethodGet, "/bookmarks", nil)
	rec := httptest.NewRecorder()

	sessionCookie := ts.createTestSession(t)
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	ts.Server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	// Should show first 10 bookmarks (page size)
	assert.Contains(t, body, "Test Article 1")
	assert.Contains(t, body, "Test Article 10")
	assert.NotContains(t, body, "Test Article 11") // Should not be on first page

	// Test that pagination links exist in the response
	assert.Contains(t, body, "Previous")
	assert.Contains(t, body, "Next")

	// Test pagination to next page using a known offset
	// Since we have 15 bookmarks and page size is 10, the offset should be the timestamp of the 10th bookmark
	// We'll use the timestamp of the 10th bookmark (which should be 10 hours ago)
	offset := time.Now().Add(-10 * time.Hour).Unix()

	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/bookmarks?direction=1&offset=%d", offset), nil)
	rec = httptest.NewRecorder()
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	ts.Server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	body = rec.Body.String()
	// Should show next page of bookmarks (articles 12-15)
	assert.Contains(t, body, "Test Article 12<")
	assert.Contains(t, body, "Test Article 15<")
	// Verify that articles from the first page are not on the second page
	assert.NotContains(t, body, "Test Article 1<")
	assert.NotContains(t, body, "Test Article 5<")
	assert.NotContains(t, body, "Test Article 10<")
}

func TestIntegration_UnauthorizedAccess(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Test accessing bookmarks without session
	req := httptest.NewRequest(http.MethodGet, "/bookmarks", nil)
	rec := httptest.NewRecorder()

	ts.Server.ServeHTTP(rec, req)

	// Should return 500 because getUserIdFromSession panics when user_id is nil
	// This is expected behavior for the current implementation
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestIntegration_BookmarkSearchHighlighting(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.cleanup()

	// Add bookmarks with specific content for search highlighting
	bookmarks := []domain.Bookmark{
		{
			URL:         "https://example.com/golang-article",
			Title:       "Golang Programming Guide",
			Description: "Learn Golang programming language",
			Tags:        "golang,programming",
			Private:     false,
			Readlater:   false,
			Created:     time.Now().Add(-24 * time.Hour),
			Updated:     time.Now().Add(-24 * time.Hour),
		},
	}

	err := ts.Store.SaveBookmarks(ts.TestUserID, bookmarks)
	require.NoError(t, err)

	// Search for "golang"
	req := httptest.NewRequest(http.MethodGet, "/bookmarks?q=golang", nil)
	rec := httptest.NewRecorder()

	sessionCookie := ts.createTestSession(t)
	req.Header.Set("Cookie", "user_session="+sessionCookie)

	ts.Server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	// Should contain highlighted search terms
	assert.Contains(t, body, "<mark>golang</mark>")
}

// Test the highlight function (keeping the original test)
func TestHighlight(t *testing.T) {
	// Test that the function correctly replaces "{{mark}}" and "{{endmark}}" with HTML <mark> tags
	input := "{{mark}}Hello, world!{{endmark}}"
	expected := "<mark>Hello, world!</mark>"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}

	// Test that the function correctly handles input that does not contain "{{mark}}" and "{{endmark}}"
	input = "Hello, world!"
	expected = "Hello, world!"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}

	// Test that the function correctly handles input that contains "{{mark}}" and "{{endmark}}" with no text in between
	input = "{{mark}}{{endmark}}"
	expected = "<mark></mark>"
	if output := highlight(input); output != expected {
		t.Errorf("highlight(%q) returned %q, expected %q", input, output, expected)
	}
}
