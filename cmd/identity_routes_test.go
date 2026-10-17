package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// TestMain provisions this package's Postgres for the router tests that reach a
// handler reading it.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// identityRouter returns the real router over an emptied test database and a
// cookie store, so a request's session can be written before it is served.
func identityRouter(t *testing.T) (*gin.Engine, sessions.Store) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	store := cookie.NewStore([]byte("secret"))
	return setupRouter(handlers.NewHandler(dbtest.Setup(t), nil), store), store
}

// sessionCookie writes value under user_id through store and returns the cookie
// that carries it, as an earlier request would have left it.
func sessionCookie(t *testing.T, store sessions.Store, value string) string {
	t.Helper()

	gin.SetMode(gin.TestMode)
	writer := gin.New()
	writer.Use(sessions.Sessions(middleware.SessionName, store))
	writer.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", value)
		if err := session.Save(); err != nil {
			t.Fatalf("failed to write the session: %v", err)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	writer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, c := range w.Result().Cookies() {
		if c.Name == middleware.SessionName {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("expected the session writer to set a cookie, got %q", w.Header().Get("Set-Cookie"))
	return ""
}

// serveRequest sends one request through router carrying cookie and body, when
// either is given.
func serveRequest(t *testing.T, router *gin.Engine, method, path, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// The auth routes carry no gate: a learner whose session this instance cannot
// place can still log in, register, verify, resend, reset and sign out.
func TestAuthRoutesServeAnUnplaceableSession(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "login", method: http.MethodPost, path: "/api/v1/login", body: `{}`, want: http.StatusBadRequest},
		{name: "register", method: http.MethodPost, path: "/api/v1/register", body: `{}`, want: http.StatusBadRequest},
		{name: "verification", method: http.MethodPost, path: "/api/v1/verification/no-such-token", want: http.StatusBadRequest},
		{name: "resend verification", method: http.MethodPost, path: "/api/v1/resend-verification", body: `{}`, want: http.StatusBadRequest},
		{name: "forgot password", method: http.MethodPost, path: "/api/v1/forgot-password", body: `{}`, want: http.StatusBadRequest},
		{name: "reset password", method: http.MethodPost, path: "/api/v1/reset-password/no-such-token", body: `{}`, want: http.StatusBadRequest},
		{name: "logout", method: http.MethodPost, path: "/api/v1/logout", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, store := identityRouter(t)
			cookie := sessionCookie(t, store, "not-a-number")

			w := serveRequest(t, router, tt.method, tt.path, cookie, tt.body)

			if w.Code != tt.want {
				t.Fatalf("expected the handler's own %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if tt.name == "logout" && !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
				t.Fatalf("expected sign-out to clear the cookie, got %q", w.Header().Get("Set-Cookie"))
			}
		})
	}
}

// Public content refuses a session this instance cannot place, rather than
// serving it as a guest.
func TestPublicContentRefusesAnUnplaceableSession(t *testing.T) {
	paths := []string{
		"/api/v1/decks/",
		"/api/v1/decks/1",
		"/api/v1/decks/shared/no-such-token",
		"/api/v1/cards/",
		"/api/v1/cards/1",
	}

	router, store := identityRouter(t)
	cookie := sessionCookie(t, store, "not-a-number")

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			w := serveRequest(t, router, http.MethodGet, path, cookie, "")

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d: %s", w.Code, w.Body.String())
			}
			if w.Body.String() != `{"error":"Unauthorized"}` {
				t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
			}
		})
	}
}

// Public content still serves a true guest — no session at all — with what a
// guest may read.
func TestPublicContentServesGuests(t *testing.T) {
	router, _ := identityRouter(t)

	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "the Deck listing", path: "/api/v1/decks/", want: http.StatusOK},
		{name: "an unknown Deck", path: "/api/v1/decks/1", want: http.StatusNotFound},
		{name: "an unknown share link", path: "/api/v1/decks/shared/no-such-token", want: http.StatusNotFound},
		{name: "the Card listing", path: "/api/v1/cards/", want: http.StatusOK},
		{name: "an unknown Card", path: "/api/v1/cards/1", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveRequest(t, router, http.MethodGet, tt.path, "", "")

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
		})
	}
}

// A private route refuses an unplaceable session in the same one shape as an
// anonymous one.
func TestPrivateRoutesRefuseAnUnplaceableSession(t *testing.T) {
	router, store := identityRouter(t)
	cookie := sessionCookie(t, store, "not-a-number")

	for _, path := range []string{"/api/v1/user/profile", "/api/v1/user/settings", "/api/v1/reviews/"} {
		t.Run(path, func(t *testing.T) {
			w := serveRequest(t, router, http.MethodGet, path, cookie, "")

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d: %s", w.Code, w.Body.String())
			}
			if w.Body.String() != `{"error":"Unauthorized"}` {
				t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
			}
		})
	}
}
