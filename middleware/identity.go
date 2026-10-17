package middleware

import (
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// Where a request's caller, and an unresolvable session, are kept between the
// step that resolves them and the handlers that ask.
const (
	callerKey     = "callerID"
	unresolvedKey = "unresolvedSession"
)

// SessionName is the cookie the identity module issues and reads.
const SessionName = "userSession"

// userIDKey is the session key the caller's learner id is stored under, as the
// decimal string the session carries.
const userIDKey = "user_id"

// SessionOptions is how the session cookie this instance issues is scoped. Sign
// in and sign out both name it, so the cookie a sign-out clears is the one a
// sign-in set — whichever route either is reached by.
func SessionOptions() sessions.Options {
	return sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 24, // 24 hours
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// SignIn records learnerID as the caller of the session this request carries.
// The session key and the cookie's scope are the identity module's, so no
// handler names either.
func SignIn(c *gin.Context, learnerID int64) error {
	session := sessions.Default(c)
	session.Set(userIDKey, strconv.FormatInt(learnerID, 10))
	session.Options(SessionOptions())
	return session.Save()
}

// SignOut ends the session this request carries and clears its cookie, in one
// shape whichever route ends it — signed in, unplaceable, or absent.
func SignOut(c *gin.Context) error {
	session := sessions.Default(c)
	session.Clear()

	options := SessionOptions()
	options.MaxAge = -1 // Expire the cookie now.
	session.Options(options)
	return session.Save()
}

// Caller returns the id of the learner making this request, resolved once from
// the session by ResolveCaller. ok is false for an anonymous request: one
// carrying no session user id, one that does not resolve, or one made outside
// the step — RequireCaller gates on the same answer.
func Caller(c *gin.Context) (int64, bool) {
	id, ok := c.Get(callerKey)
	if !ok {
		return access.Anonymous, false
	}

	caller, ok := id.(int64)
	if !ok {
		return access.Anonymous, false
	}
	return caller, true
}

// unresolvedSession reports whether the session named a learner this instance
// could not place — an id that is not the decimal string this instance writes,
// an id that does not parse, or the anonymous id. Such a session is no caller,
// and a route that serves anonymous callers answers it 401 rather than serving
// it as a guest, so a learner whose session this instance cannot read is told to
// sign in again instead of being handed a guest's view. A private route is
// refused by RequireCaller either way.
func unresolvedSession(c *gin.Context) bool {
	unresolved, ok := c.Get(unresolvedKey)
	if !ok {
		return false
	}

	flagged, ok := unresolved.(bool)
	return ok && flagged
}

// RefuseUnauthorized answers a request in the one shape an unresolvable caller is
// refused in, whoever refuses it.
func RefuseUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
}

// ResolveCaller produces the caller once, for every route in the group it is
// applied to. It never refuses a request: a session carrying no learner id, or
// one that does not parse, is an anonymous caller, and what happens to that is
// the group's gate's question — AllowGuest or RequireCaller.
func ResolveCaller() gin.HandlerFunc {
	return func(c *gin.Context) {
		caller, carried, resolved := resolveCaller(c)
		switch {
		case resolved:
			c.Set(callerKey, caller)
		case carried:
			c.Set(unresolvedKey, true)
		}
		c.Next()
	}
}

// AllowGuest gates routes that serve anonymous callers. A true guest is served:
// no session at all, or one carrying no user id. A session carrying a user id
// this instance cannot place is refused rather than served as a guest, so the
// learner is told to sign in again instead of being handed a guest's view.
func AllowGuest() gin.HandlerFunc {
	return func(c *gin.Context) {
		if unresolvedSession(c) {
			RefuseUnauthorized(c)
			return
		}
		c.Next()
	}
}

// RequireCaller gates the routes it is applied to on a resolved caller, refusing
// the rest in one shape, whatever handler sits behind the route.
func RequireCaller() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := Caller(c); !ok {
			RefuseUnauthorized(c)
			return
		}
		c.Next()
	}
}

// resolveCaller reads the learner id the session carries as the decimal string it
// was stored as. resolved is false when there is no caller to have: no user id,
// one that is not that string, one that does not parse, or the anonymous id —
// learner ids come from a BIGSERIAL and are never zero. carried tells a session
// that named a learner this instance cannot place from no session at all.
func resolveCaller(c *gin.Context) (caller int64, carried, resolved bool) {
	value := sessions.Default(c).Get(userIDKey)
	if value == nil {
		return access.Anonymous, false, false
	}

	raw, ok := value.(string)
	if !ok {
		return access.Anonymous, true, false
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id == access.Anonymous {
		return access.Anonymous, true, false
	}
	return id, true, true
}
