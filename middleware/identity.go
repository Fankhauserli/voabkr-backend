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

// Caller returns the id of the learner making this request, resolved once from
// the session by ResolveCaller. ok is false for an anonymous request: one
// carrying no session user id, one that does not resolve, or one made outside
// the step — the private route group gates on the same answer.
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

// UnresolvedSession reports whether the session named a learner this instance
// could not place — an id that does not parse, or the anonymous id. Such a
// session is no caller, and a route that serves anonymous callers answers it 401
// rather than serving it as a guest, so a learner whose session this instance
// cannot read is told to sign in again instead of being handed a guest's view.
// A private route is refused by RequireCaller either way.
func UnresolvedSession(c *gin.Context) bool {
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
// one that does not parse, is an anonymous caller, and who may act on that is
// RequireCaller's question and the handlers'.
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
// was stored as. resolved is false when there is no caller to have: no session,
// no id in it, an id that does not parse, or the anonymous id — learner ids come
// from a BIGSERIAL and are never zero. carried tells a session that named a
// learner this instance cannot place from no session at all.
func resolveCaller(c *gin.Context) (caller int64, carried, resolved bool) {
	raw, ok := sessions.Default(c).Get("user_id").(string)
	if !ok {
		return access.Anonymous, false, false
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id == access.Anonymous {
		return access.Anonymous, true, false
	}
	return id, true, true
}
