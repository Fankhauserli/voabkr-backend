package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-gonic/gin"
)

// requireCaller returns the id of the learner making this request, answering 401
// when it carries none and leaving ok false once it has. The private route group
// gates on the same answer, in the same shape, so this is what a handler refuses
// — and what covers one reached without the group's gate.
//
// The caller is resolved from the session once, by middleware.ResolveCaller;
// handlers ask middleware.Caller for them and never parse the session.
func requireCaller(c *gin.Context) (int64, bool) {
	caller, ok := middleware.Caller(c)
	if !ok {
		middleware.RefuseUnauthorized(c)
		return access.Anonymous, false
	}
	return caller, true
}

// How settling a Deck read can end without the caller being allowed in. It does
// not answer the request: the words a refusal is put in belong to whoever asked.
var errDeckNotReadable = errors.New("Deck not readable by this caller")

// deckAccess settles whether caller may read deckID, without answering the
// request. A batch needs this: it refuses one entry at a time, so the refusal
// cannot be the request's. The Deck itself is read, and the caller's Access role
// resolved on it, by the Deck reader.
func (h *Handler) deckAccess(ctx context.Context, deckID, caller int64) (decks.Standing, error) {
	standing, err := h.Decks.StandingOn(ctx, deckID, caller)
	if err != nil {
		return decks.Standing{}, err
	}
	if !access.CanRead(standing.Deck, standing.Role) {
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
func (h *Handler) requireReadableDeck(c *gin.Context, deckID int64, caller int64) (decks.Standing, bool) {
	standing, err := h.deckAccess(c.Request.Context(), deckID, caller)
	switch {
	case errors.Is(err, decks.ErrNoSuchDeck):
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return decks.Standing{}, false
	case errors.Is(err, errDeckNotReadable):
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
		return decks.Standing{}, false
	case err != nil:
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Deck", err)
		return decks.Standing{}, false
	}

	return standing, true
}

// requireDeckEdit loads a Deck and settles whether the caller may change it or
// what it holds — its name and type, and its Cards — resolving their Access role
// the same way a read does and asking access/ what that standing permits. It
// answers the request itself: 404 when there is no such Deck, and the refusal
// the act asked for once the caller's standing does not carry the edit.
//
// Every edit of a Deck or a Card goes through here, so the rule is applied in
// one place rather than re-spelled per handler.
func (h *Handler) requireDeckEdit(c *gin.Context, deckID int64, caller int64, refusal string) (decks.Standing, bool) {
	standing, err := h.Decks.StandingOn(c.Request.Context(), deckID, caller)
	switch {
	case errors.Is(err, decks.ErrNoSuchDeck):
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return decks.Standing{}, false
	case err != nil:
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Deck", err)
		return decks.Standing{}, false
	}

	if !access.CanEdit(standing.Deck, standing.Role) {
		c.JSON(http.StatusForbidden, gin.H{"error": refusal})
		return standing, false
	}

	return standing, true
}

// requireDeckOwner loads a Deck and settles whether the caller may act on it as
// its owner — delete it, share it, revoke one of its DeckShares. It answers the
// request itself: 404 when there is no such Deck, and the refusal the act asked
// for once the caller does not own it.
//
// Ownership is asked of access/, which fails closed: a System deck has no owner
// for anyone to be, so it refuses every one of these acts.
func (h *Handler) requireDeckOwner(c *gin.Context, deckID, caller int64, refusal string) (decks.Standing, bool) {
	standing, err := h.Decks.StandingOn(c.Request.Context(), deckID, caller)
	switch {
	case errors.Is(err, decks.ErrNoSuchDeck):
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return decks.Standing{}, false
	case err != nil:
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Deck", err)
		return decks.Standing{}, false
	}

	if !access.Owns(standing.Deck, caller) {
		c.JSON(http.StatusForbidden, gin.H{"error": refusal})
		return standing, false
	}

	return standing, true
}
