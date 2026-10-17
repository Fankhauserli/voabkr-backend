package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const whoamiRoute = "/whoami"

// identityRouter serves whoamiRoute behind the identity steps, recording the
// caller they resolved. gate is the group's gate, or nil for the ungated auth
// routes.
func identityRouter(gate gin.HandlerFunc, seen *int64, signedIn *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions(middleware.SessionName, cookie.NewStore([]byte("secret"))))
	router.Use(middleware.ResolveCaller())

	// The session a learner arrives with is written by an earlier request, so the
	// cookie it is given is what carries it into whoamiRoute. An `as` of "" is a
	// session carrying an empty user id; `kind=int` writes a non-string id.
	router.GET("/signin", func(c *gin.Context) {
		if raw, ok := c.GetQuery("as"); ok {
			var value any = raw
			if c.Query("kind") == "int" {
				value, _ = strconv.ParseInt(raw, 10, 64)
			}
			sessions.Default(c).Set("user_id", value)
			if err := sessions.Default(c).Save(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.Status(http.StatusOK)
	})

	// whoamiRoute stands in for a route group: resolved for everyone, gated as
	// the group is.
	group := router.Group("/")
	if gate != nil {
		group.Use(gate)
	}
	group.GET(whoamiRoute, func(c *gin.Context) {
		*seen, *signedIn = middleware.Caller(c)
		c.Status(http.StatusOK)
	})

	return router
}

// serve sends one request through router, carrying raw as the session's user_id
// when it is not nil.
func serve(t *testing.T, router *gin.Engine, raw *string) *httptest.ResponseRecorder {
	t.Helper()

	if raw == nil {
		return serveSession(t, router, "")
	}
	return serveSession(t, router, "as="+*raw)
}

// serveSession writes the session described by the /signin query and sends
// whoamiRoute with the cookie it set. An empty query sends no session at all.
func serveSession(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()

	cookie := ""
	if query != "" {
		signin := httptest.NewRecorder()
		router.ServeHTTP(signin, httptest.NewRequest(http.MethodGet, "/signin?"+query, nil))
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

// sessionRouter serves /signin, /signout and a private /whoami, so a test can
// follow the cookie the identity module issues across requests.
func sessionRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions(middleware.SessionName, cookie.NewStore([]byte("secret"))))
	router.Use(middleware.ResolveCaller())

	router.POST("/signin", func(c *gin.Context) {
		if err := middleware.SignIn(c, 42); err != nil {
			t.Errorf("sign in: %v", err)
		}
		c.Status(http.StatusOK)
	})
	router.POST("/signout", func(c *gin.Context) {
		if err := middleware.SignOut(c); err != nil {
			t.Errorf("sign out: %v", err)
		}
		c.Status(http.StatusOK)
	})

	// write-session lets a test arrive with a session this instance may not be
	// able to place, as an earlier request would have left it.
	router.POST("/write-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", c.Query("as"))
		if err := session.Save(); err != nil {
			t.Errorf("write session: %v", err)
		}
		c.Status(http.StatusOK)
	})

	private := router.Group("/")
	private.Use(middleware.RequireCaller())
	private.GET("/whoami", func(c *gin.Context) {
		caller, _ := middleware.Caller(c)
		c.JSON(http.StatusOK, gin.H{"id": caller})
	})

	return router
}

// request sends one request through router carrying cookie, when it is not
// empty.
func request(t *testing.T, router *gin.Engine, method, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// theCookie returns the named cookie the response set, failing the test when it
// set none.
func theCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()

	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("expected a %s cookie in the response, got %q", name, w.Header().Get("Set-Cookie"))
	return nil
}

// SignIn records the caller under the session key the identity module owns, in
// the cookie the module scopes, so a request following it is served as that
// learner.
func TestSignIn(t *testing.T) {
	router := sessionRouter(t)

	signedIn := request(t, router, http.MethodPost, "/signin", "")
	if signedIn.Code != http.StatusOK {
		t.Fatalf("expected sign in to succeed, got %d: %s", signedIn.Code, signedIn.Body.String())
	}

	cookie := theCookie(t, signedIn, middleware.SessionName)
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected the session cookie to carry the identity policy, got %+v", cookie)
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("expected a session cookie that outlives the request, got MaxAge %d", cookie.MaxAge)
	}

	whoami := request(t, router, http.MethodGet, "/whoami", cookie.Name+"="+cookie.Value)
	if whoami.Code != http.StatusOK {
		t.Fatalf("expected the signed-in caller to be served, got %d: %s", whoami.Code, whoami.Body.String())
	}
	if whoami.Body.String() != `{"id":42}` {
		t.Fatalf("expected caller 42, got %s", whoami.Body.String())
	}
}

// SignOut clears the session cookie in one shape whatever the request carried —
// a resolved session, an unplaceable one, or none at all — so a learner whose
// session this instance cannot read can still sign out.
func TestSignOut(t *testing.T) {
	tests := []struct {
		name string
		raw  *string
	}{
		{name: "a resolved session", raw: raw("42")},
		{name: "an unplaceable session", raw: raw("not-a-number")},
		{name: "no session at all", raw: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := sessionRouter(t)

			cookie := ""
			if tt.raw != nil {
				// A real client arrives with whatever its earlier request set, so
				// the cookie is written directly rather than through SignIn.
				cookie = writeSession(t, router, *tt.raw)
			}

			signedOut := request(t, router, http.MethodPost, "/signout", cookie)
			if signedOut.Code != http.StatusOK {
				t.Fatalf("expected sign out to succeed, got %d: %s", signedOut.Code, signedOut.Body.String())
			}

			cleared := theCookie(t, signedOut, middleware.SessionName)
			if !cleared.Expires.Before(time.Now()) || cleared.Path != "/" {
				t.Fatalf("expected the session cookie to be expired on the path it was set, got %+v", cleared)
			}

			// The same cleared cookie buys nothing on a private route.
			whoami := request(t, router, http.MethodGet, "/whoami", cleared.Name+"="+cleared.Value)
			if whoami.Code != http.StatusUnauthorized {
				t.Fatalf("expected the caller to be gone, got %d: %s", whoami.Code, whoami.Body.String())
			}
		})
	}
}

// writeSession writes raw as the session's user_id through a request of its own
// and returns the cookie that carried it.
func writeSession(t *testing.T, router *gin.Engine, value string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/write-session?as="+value, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to write the session: %d: %s", w.Code, w.Body.String())
	}
	c := theCookie(t, w, middleware.SessionName)
	return c.Name + "=" + c.Value
}

// The caller is resolved once, from the session, for every route: the id a
// session carries is what handlers ask for, and a session that carries none, or
// one that does not parse, is an anonymous caller rather than an error.
func TestResolveCaller(t *testing.T) {
	tests := []struct {
		name     string
		raw      *string
		want     int64
		signedIn bool
	}{
		{name: "a session carrying a learner id", raw: raw("42"), want: 42, signedIn: true},
		{name: "no session at all", raw: nil, want: access.Anonymous},
		{name: "a session carrying an empty learner id", raw: raw(""), want: access.Anonymous},
		{name: "a session carrying an unparseable learner id", raw: raw("not-a-number"), want: access.Anonymous},
		{name: "a session carrying the anonymous id", raw: raw("0"), want: access.Anonymous},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen int64
			var signedIn bool
			router := identityRouter(nil, &seen, &signedIn)

			w := serve(t, router, tt.raw)

			// An ungated route serves whoever arrives, resolved or not.
			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200 on an ungated route, got %d: %s", w.Code, w.Body.String())
			}
			if signedIn != tt.signedIn {
				t.Fatalf("expected signed in: %v, got %v", tt.signedIn, signedIn)
			}
			if seen != tt.want {
				t.Fatalf("expected caller %d, got %d", tt.want, seen)
			}
		})
	}
}

// A route that serves guests answers an unplaceable session 401 in the one
// refusal shape rather than serving it as an anonymous caller; a true guest is
// still served.
func TestAllowGuest(t *testing.T) {
	tests := []struct {
		name       string
		raw        *string
		wantStatus int
	}{
		{name: "a session carrying a learner id", raw: raw("42"), wantStatus: http.StatusOK},
		{name: "no session at all", raw: nil, wantStatus: http.StatusOK},
		{name: "a session carrying an empty learner id", raw: raw(""), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying an unparseable learner id", raw: raw("not-a-number"), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying the anonymous id", raw: raw("0"), wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen int64
			var signedIn bool
			router := identityRouter(middleware.AllowGuest(), &seen, &signedIn)

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

// A session whose user id is not the decimal string this instance writes cannot
// be placed, even though it resembles a learner id.
func TestAllowGuestRefusesANonStringLearnerID(t *testing.T) {
	var seen int64
	var signedIn bool
	router := identityRouter(middleware.AllowGuest(), &seen, &signedIn)

	w := serveSession(t, router, "as=42&kind=int")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for a non-string learner id, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"error":"Unauthorized"}` {
		t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
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
		{name: "a session carrying an empty learner id", raw: raw(""), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying an unparseable learner id", raw: raw("not-a-number"), wantStatus: http.StatusUnauthorized},
		{name: "a session carrying the anonymous id", raw: raw("0"), wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen int64
			var signedIn bool
			router := identityRouter(middleware.RequireCaller(), &seen, &signedIn)

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
