package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// newTestHandler returns a handler over the test database with every table
// emptied, and no cache.
func newTestHandler(t *testing.T) *handlers.Handler {
	t.Helper()
	return handlers.NewHandler(dbtest.Setup(t), nil)
}

// newTestHandlerWithCache returns a handler over the test database with every
// table emptied, and a local-only cache, so a test can watch what a cache entry
// does across two callers.
func newTestHandlerWithCache(t *testing.T) *handlers.Handler {
	t.Helper()
	queries := dbtest.Setup(t)

	c, err := cache.NewTwoLevelCache(cache.Config{EnableLocalCache: true})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(c.Close)

	return handlers.NewHandler(queries, c)
}

// signedIn installs the session a signed-in learner's request carries.
func signedIn(userID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", strconv.FormatInt(userID, 10))
		_ = session.Save()
	}
}

// serve sends method to path through handler as callerID — access.Anonymous
// sends no session at all — and returns what the caller saw. A non-empty body is
// sent as JSON.
//
// The chain is the router's: the session a signed-in learner's request carries
// is established, the identity step resolves the caller from it, and only then
// does the handler ask.
func serve(t *testing.T, handler gin.HandlerFunc, callerID int64, method, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	chain := make([]gin.HandlerFunc, 0, 3)
	if callerID != access.Anonymous {
		chain = append(chain, signedIn(callerID))
	}
	chain = append(chain, middleware.ResolveCaller(), handler)

	router := setupTestRouter()
	router.Handle(method, route, chain...)

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// get serves route at path through handler as callerID, and returns what the
// caller saw.
func get(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodGet, route, path, "")
}

// post serves route at path through handler as callerID with a JSON body, and
// returns what the caller saw.
func post(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodPost, route, path, body)
}

// put serves route at path through handler as callerID with a JSON body, and
// returns what the caller saw.
func put(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodPut, route, path, body)
}

// del serves route at path through handler as callerID, and returns what the
// caller saw.
func del(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodDelete, route, path, "")
}
