package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

// callerID returns the id of the learner making this request. ok is false for an
// anonymous request: one carrying no session user id, or one that does not parse.
func callerID(c *gin.Context) (id int64, ok bool) {
	raw, ok := sessions.Default(c).Get("user_id").(string)
	if !ok {
		return access.Anonymous, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return access.Anonymous, false
	}
	return id, true
}

// deckStanding is a Deck with the Access role the caller holds on it, whether or
// not that standing permits what they asked for.
type deckStanding struct {
	deck db.Deck
	role string
}

// How settling a Deck read can end without the caller being allowed in. Neither
// answers the request: the words a refusal is put in belong to whoever asked.
var (
	errNoSuchDeck      = errors.New("no such Deck")
	errDeckNotReadable = errors.New("Deck not readable by this caller")
)

// deckStandingOn reads deckID and resolves the caller's Access role on it from
// the Deck and the caller's own DeckShare, judging nothing by it.
func (h *Handler) deckStandingOn(ctx context.Context, deckID, caller int64) (deckStanding, error) {
	deck, err := h.DB.GetDeck(ctx, deckID)
	if err != nil {
		return deckStanding{}, fmt.Errorf("%w: %v", errNoSuchDeck, err)
	}

	var share *db.DeckShare
	if caller != access.Anonymous && !access.Owns(deck, caller) {
		if s, err := h.DB.GetDeckShare(ctx, db.GetDeckShareParams{
			DeckID: deck.ID,
			UserID: pgtype.Int8{Int64: caller, Valid: true},
		}); err == nil {
			share = &s
		}
	}

	return deckStanding{deck: deck, role: access.Role(deck, share, caller)}, nil
}

// deckAccess settles whether caller may read deckID, without answering the
// request. A batch needs this: it refuses one entry at a time, so the refusal
// cannot be the request's.
func (h *Handler) deckAccess(ctx context.Context, deckID, caller int64) (deckStanding, error) {
	standing, err := h.deckStandingOn(ctx, deckID, caller)
	if err != nil {
		return deckStanding{}, err
	}
	if !access.CanRead(standing.deck, standing.role) {
		return standing, errDeckNotReadable
	}
	return standing, nil
}

// requireReadableDeck loads a Deck and settles whether the caller may read it,
// resolving their Access role from the Deck and their own DeckShare. It answers
// the request itself: 404 when there is no such Deck, 403 when there is but the
// caller may not read it — ok is false once it has answered one of those.
//
// Every read of a Deck or its Cards goes through here, so the rule is applied in
// one place rather than re-spelled per handler.
func (h *Handler) requireReadableDeck(c *gin.Context, deckID int64, caller int64) (deckStanding, bool) {
	standing, err := h.deckAccess(c.Request.Context(), deckID, caller)
	switch {
	case errors.Is(err, errNoSuchDeck):
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return deckStanding{}, false
	case err != nil:
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
		return deckStanding{}, false
	}

	return standing, true
}

// requireDeckWrite loads a Deck and settles whether the caller may write its
// Cards — add one, change one, delete one — resolving their Access role the same
// way a read does and asking access/ what that standing permits. It answers the
// request itself, and ok is false once it has: 404 when there is no such Deck,
// 403 when there is but the caller's standing does not carry the write.
//
// Every write of a Card goes through here, so the rule is applied in one place
// rather than re-spelled per handler.
func (h *Handler) requireDeckWrite(c *gin.Context, deckID int64, caller int64) bool {
	// The only way resolving a standing fails is a Deck that isn't there.
	standing, err := h.deckStandingOn(c.Request.Context(), deckID, caller)
	if err != nil {
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return false
	}

	if !access.CanWriteCards(standing.deck, standing.role) {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not have permission to edit this deck's Cards"})
		return false
	}

	return true
}

// callerParam carries a caller id into a query that filters by access. An
// anonymous caller is NULL, which matches no owner and no DeckShare.
func callerParam(caller int64) pgtype.Int8 {
	return pgtype.Int8{Int64: caller, Valid: caller != access.Anonymous}
}
