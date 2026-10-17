package handlers_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
)

// Signing out ends the session identically whether the learner asked the logout
// route or erased their account: one cookie, expired on the path it was set.
func TestSignOutClearsTheCookieIdentically(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	loggedOut := post(t, h.Logout, learner, "/api/v1/logout", "/api/v1/logout", "")
	erased := del(t, h.DeleteAccount, learner, "/api/v1/user", "/api/v1/user")

	logoutCookie := clearedCookieAttributes(t, loggedOut)
	erasureCookie := clearedCookieAttributes(t, erased)

	if logoutCookie != erasureCookie {
		t.Fatalf("expected one clearing cookie from every sign-out path:\nlogout:  %q\nerasure: %q", logoutCookie, erasureCookie)
	}
	if !strings.Contains(logoutCookie, "Path=/") || !strings.Contains(logoutCookie, "Max-Age=0") {
		t.Fatalf("expected the session cookie to be expired on the root path, got %q", logoutCookie)
	}
}

// clearedCookieAttributes returns the attributes of the final Set-Cookie a response
// carries — the one its handler wrote, after any the request's own sign-in
// wrote. The value is left out: it differs per request while the attributes are
// the sign-out policy.
func clearedCookieAttributes(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	cookies := w.Header().Values("Set-Cookie")
	if len(cookies) == 0 {
		t.Fatalf("expected a Set-Cookie header in the response")
	}

	attributes := strings.SplitN(cookies[len(cookies)-1], ";", 2)
	if len(attributes) != 2 {
		t.Fatalf("expected a cookie with attributes, got %q", cookies[len(cookies)-1])
	}
	return attributes[1]
}
