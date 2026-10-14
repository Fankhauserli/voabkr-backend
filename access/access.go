// Package access answers what standing a learner holds on a Deck, and what that
// standing permits. It is pure: callers fetch the Deck and the caller's
// DeckShare and ask, rather than branching on a role themselves.
package access

import (
	"github.com/Fankhauserli/voabkr-backend/sql/db"
)

// The four Access roles. These strings are the wire vocabulary — learners'
// clients read them — so they are the same constants ADR-0004 fixes in prose.
const (
	RoleOwner  = "owner"
	RoleEdit   = "edit"
	RoleView   = "view"
	RolePublic = "public"
)

// Anonymous is the caller id of a learner who is not signed in. Learner ids
// come from a BIGSERIAL and are never zero.
const Anonymous int64 = 0

// Owns reports whether callerID is deck's Deck owner. An anonymous caller owns
// nothing, and a System deck has no owner for anyone to be.
func Owns(deck db.Deck, callerID int64) bool {
	return callerID != Anonymous && deck.OwnerID.Valid && deck.OwnerID.Int64 == callerID
}

// Role resolves callerID's Access role on deck. share is the caller's own
// DeckShare on that Deck, or nil when they hold none; a share belonging to
// another learner, or to another Deck, is ignored rather than trusted.
func Role(deck db.Deck, share *db.DeckShare, callerID int64) string {
	if callerID == Anonymous {
		return RolePublic
	}
	if Owns(deck, callerID) {
		return RoleOwner
	}
	if share != nil &&
		share.DeckID == deck.ID &&
		share.UserID.Valid && share.UserID.Int64 == callerID &&
		share.Permission != "" {
		return share.Permission
	}
	return RolePublic
}

// CanRead reports whether a caller holding role may read deck: it is public, or
// they own it or hold a DeckShare on it. This is the rule the listing queries
// filter on, so a Deck unreadable here is one their Card listings omit too.
func CanRead(deck db.Deck, role string) bool {
	return deck.IsPublic || role != RolePublic
}
