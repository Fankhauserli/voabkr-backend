package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// unplaceableSession is the session a client whose learner id this instance
// cannot read arrives with.
const unplaceableSession = "not-a-number"

// serveCaller routes a request whose session carries raw under user_id through
// the identity step — the chain the router builds — into handler.
func serveCaller(t *testing.T, handler gin.HandlerFunc, raw, method, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	router := setupTestRouter()
	router.Handle(method, route,
		func(c *gin.Context) {
			session := sessions.Default(c)
			session.Set("user_id", raw)
			_ = session.Save()
		},
		middleware.ResolveCaller(),
		handler,
	)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// A private handler reached with a session naming a learner this instance cannot
// read answers 401, not 400: the caller is not there, and the request is not the
// learner's fault.
func TestPrivateHandlerRefusesAnUnplaceableSession(t *testing.T) {
	h := newTestHandler(t)

	w := serveCaller(t, h.GetSettings, unplaceableSession, http.MethodGet, "/api/v1/user/settings", "/api/v1/user/settings", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for an unplaceable session, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"error":"Unauthorized"}` {
		t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
	}
}

// A route that serves guests answers an unplaceable session 401 rather than a
// guest's listing: the learner is told to sign in again, which a guest's view
// would not tell them.
func TestPublicRoutesRefuseAnUnplaceableSession(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*handlers.Handler) gin.HandlerFunc
		method  string
		route   string
		path    string
		body    string
	}{
		{
			name:    "the Deck listing",
			handler: func(h *handlers.Handler) gin.HandlerFunc { return h.GetDecks },
			method:  http.MethodGet,
			route:   decksRoute,
			path:    decksRoute,
		},
		{
			name:    "resending a verification email",
			handler: func(h *handlers.Handler) gin.HandlerFunc { return h.ResendVerificationEmail },
			method:  http.MethodPost,
			route:   "/api/v1/resend-verification",
			path:    "/api/v1/resend-verification",
			body:    `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			dbtest.SeedDeck(t, learner, true)

			w := serveCaller(t, tt.handler(h), unplaceableSession, tt.method, tt.route, tt.path, tt.body)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401 for an unplaceable session, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// The guest view is still served to a request that carries no session at all —
// the refusal above is about a session this instance cannot place, not about
// being signed out.
func TestPublicRoutesStillServeGuests(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	dbtest.SeedDeck(t, learner, true)

	w := get(t, h.GetDecks, access.Anonymous, decksRoute, decksRoute)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for a guest, got %d: %s", w.Code, w.Body.String())
	}
}
