package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const whoamiRoute = "/whoami"

// identityRouter serves whoamiRoute behind the identity steps, recording the
// caller they resolved.
func identityRouter(required bool, seen *int64, signedIn, unresolved *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("userSession", cookie.NewStore([]byte("secret"))))
	router.Use(middleware.ResolveCaller())

	// The session a learner arrives with is written by an earlier request, so the
	// cookie it is given is what carries it into whoamiRoute.
	router.GET("/signin", func(c *gin.Context) {
		if raw := c.Query("as"); raw != "" {
			sessions.Default(c).Set("user_id", raw)
			if err := sessions.Default(c).Save(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.Status(http.StatusOK)
	})

	// whoamiRoute stands in for the private group: resolved for everyone, gated
	// on having been.
	group := router.Group("/")
	if required {
		group.Use(middleware.RequireCaller())
	}
	group.GET(whoamiRoute, func(c *gin.Context) {
		*seen, *signedIn = middleware.Caller(c)
		*unresolved = middleware.UnresolvedSession(c)
		c.Status(http.StatusOK)
	})

	return router
}

// serve sends one request through router, carrying raw as the session's user_id
// when it is not nil.
func serve(t *testing.T, router *gin.Engine, raw *string) *httptest.ResponseRecorder {
	t.Helper()

	cookie := ""
	if raw != nil {
		signin := httptest.NewRecorder()
		router.ServeHTTP(signin, httptest.NewRequest(http.MethodGet, "/signin?as="+*raw, nil))
		if signin.Code != http.StatusOK {
			t.Fatalf("failed to sign in: %d: %s", signin.Code, signin.Body.String())
		}
		for _, c := range signin.Result().Cookies() {
			cookie = c.Name + "=" + c.Value
		}
	}

	req := httptest.NewRequest(http.MethodGet, whoamiRoute, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func raw(value string) *string { return &value }

// The caller is resolved once, from the session, for every route: the id a
// session carries is what handlers ask for, and a session that carries none, or
// one that does not parse, is an anonymous caller rather than an error.
func TestResolveCaller(t *testing.T) {
	tests := []struct {
		name       string
		raw        *string
		want       int64
		signedIn   bool
		unresolved bool
	}{
		{name: "a session carrying a learner id", raw: raw("42"), want: 42, signedIn: true},
		{name: "no session at all", raw: nil, want: access.Anonymous},
		{name: "a session carrying no learner id", raw: raw(""), want: access.Anonymous},
		{name: "a session carrying an unparseable learner id", raw: raw("not-a-number"), want: access.Anonymous, unresolved: true},
		{name: "a session carrying the anonymous id", raw: raw("0"), want: access.Anonymous, unresolved: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen int64
			var signedIn, unresolved bool
			router := identityRouter(false, &seen, &signedIn, &unresolved)

			w := serve(t, router, tt.raw)

			// A public route serves whoever arrives, resolved or not.
			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200 on a public route, got %d: %s", w.Code, w.Body.String())
			}
			if signedIn != tt.signedIn {
				t.Fatalf("expected signed in: %v, got %v", tt.signedIn, signedIn)
			}
			if seen != tt.want {
				t.Fatalf("expected caller %d, got %d", tt.want, seen)
			}
			if unresolved != tt.unresolved {
				t.Fatalf("expected unresolved session: %v, got %v", tt.unresolved, unresolved)
			}
		})
	}
}

// A private route is refused in one shape when it carries no caller, whether the
// session is absent or unparseable, and served when it carries one.
func TestRequireCaller(t *testing.T) {
	tests := []struct {
		name       string
		raw        *string
		wantStatus int
	}{
		{name: "a session carrying a learner id", raw: raw("42"), wantStatus: http.StatusOK},
		{name: "no session at all", raw: nil, wantStatus: http.StatusUnauthorized},
		{name: "a session carrying no learner id", raw: raw(""), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying an unparseable learner id", raw: raw("not-a-number"), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying the anonymous id", raw: raw("0"), wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen int64
			var signedIn, unresolved bool
			router := identityRouter(true, &seen, &signedIn, &unresolved)

			w := serve(t, router, tt.raw)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantStatus == http.StatusUnauthorized && w.Body.String() != `{"error":"Unauthorized"}` {
				t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
			}
		})
	}
}
