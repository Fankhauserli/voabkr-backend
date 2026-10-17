// Package cards owns reading Cards end to end: the listings, the single read,
// the Review schedule each Card carries for the caller asking, the shape a Card
// takes on the wire, and which entries a write invalidates. Callers ask for
// Cards rather than assembling them, and hold no key.
//
// An entry holds a Deck's Cards — data every caller reads alike — and never the
// Review schedule of whoever warmed it, which is attached per request (ADR-0001).
package cards

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// entryTTL is how long an entry holding a Deck's Cards lives. A write clears what
// it holds, so this bounds only what nothing writes through.
const entryTTL = 5 * time.Minute

// ErrNoSuchCard is returned for a Card that is not there, and for one the
// listings no longer carry.
var ErrNoSuchCard = errors.New("no such Card")

// ErrSchedules wraps a failure to read the caller's own Review schedules after
// the Cards themselves were read, so a handler can answer in the words its
// route has always used for that failure.
var ErrSchedules = errors.New("failed to read the Review schedules")

// Reader answers every read of a Card, and clears what a write invalidates.
type Reader struct {
	db    *db.Queries
	cache cache.Cache
}

// NewReader returns a Reader over the database a Card is read from and the cache
// it is held in. A nil cache means an instance that holds nothing.
func NewReader(q *db.Queries, c cache.Cache) *Reader {
	return &Reader{db: q, cache: c}
}

// Page is the window a listing is asked for. Paged tells a request that named a
// limit or an offset at all: one that named neither is answered with every Card,
// as it was before paging existed.
type Page struct {
	Limit  int32
	Offset int32
	Paged  bool
}

// The Card keys are built here and nowhere else. A Deck's Cards live under the
// Deck, and a paged read under the window it asked for.
func deckKey(deckID int64) string { return fmt.Sprintf("cards:deck:%d", deckID) }

func deckPageKey(deckID int64, page Page) string {
	return fmt.Sprintf("%s:lim%d:off%d", deckKey(deckID), page.Limit, page.Offset)
}

// Card is a Card with what a request about it needs beside its body: the Deck
// that holds it, which is what decides who may read it at all.
type Card struct {
	// Body is the Card as the wire carries it, carrying the caller's own Review
	// schedule by the time ByID returns.
	Body *types.Card

	// DeckID is the Deck that holds the Card.
	DeckID int64
}

// ByID returns cardID as caller reads it, carrying their own Review schedule of
// it, and the Deck that holds it. A Card that is not there — deleted included,
// which the query reads as gone — is ErrNoSuchCard.
func (r *Reader) ByID(ctx context.Context, caller int64, cardID int64) (Card, error) {
	row, err := r.db.GetCard(ctx, cardID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Card{}, ErrNoSuchCard
	case err != nil:
		// A database that cannot answer is not a Card that is not there.
		return Card{}, fmt.Errorf("failed to read the Card: %w", err)
	}

	body := bodyOf(row)
	if err := r.attachSchedules(ctx, caller, []*types.Card{body}); err != nil {
		return Card{}, err
	}
	return Card{Body: body, DeckID: row.DeckID}, nil
}

// ByDeck returns the Deck's Cards as caller reads them: the Deck's contents,
// which are the same for all, with the caller's own Review schedule of each.
// An entry answers the contents; the schedules are attached per request and
// never held in it (ADR-0001).
func (r *Reader) ByDeck(ctx context.Context, caller int64, deckID int64, page Page) ([]*types.Card, error) {
	key := deckKey(deckID)
	if page.Paged {
		key = deckPageKey(deckID, page)
	}
	if r.cache != nil {
		var cached types.CardList
		if found, err := r.cache.GetProto(ctx, key, &cached); err == nil && found {
			if err := r.attachSchedules(ctx, caller, cached.Cards); err != nil {
				return nil, err
			}
			return cached.Cards, nil
		}
	}

	var (
		rows []db.Card
		err  error
	)
	if page.Paged {
		rows, err = r.db.ListCardsByDeck(ctx, db.ListCardsByDeckParams{
			DeckID: deckID,
			Limit:  page.Limit,
			Offset: page.Offset,
		})
	} else {
		rows, err = r.db.ListCardsByDeckAll(ctx, deckID)
	}
	if err != nil {
		return nil, err
	}

	cards := bodies(rows)
	// The entry holds the Deck's contents alone, so it is written before the
	// caller's schedules are attached.
	if r.cache != nil {
		_ = r.cache.SetProto(ctx, key, &types.CardList{Cards: cards}, entryTTL)
	}
	if err := r.attachSchedules(ctx, caller, cards); err != nil {
		return nil, err
	}
	return cards, nil
}

// ForCaller returns the Cards of every Deck caller may read, each carrying the
// caller's own Review schedule. Which Decks those are is a decision about the
// caller, so nothing answers this from an entry.
func (r *Reader) ForCaller(ctx context.Context, caller int64, page Page) ([]*types.Card, error) {
	var (
		rows []db.Card
		err  error
	)
	if page.Paged {
		rows, err = r.db.ListAccessibleCardsPaged(ctx, db.ListAccessibleCardsPagedParams{
			UserID:     callerParam(caller),
			PageLimit:  page.Limit,
			PageOffset: page.Offset,
		})
	} else {
		rows, err = r.db.ListAccessibleCards(ctx, callerParam(caller))
	}
	if err != nil {
		return nil, err
	}

	return r.FromRows(ctx, caller, rows)
}

// FromRows renders rows a caller's own query read — the Review queue reads its
// Cards itself — as the wire carries them, carrying the caller's Review
// schedules. A caller asks for Cards, rather than attaching their schedules
// afterwards and hoping every read remembers to.
func (r *Reader) FromRows(ctx context.Context, caller int64, rows []db.Card) ([]*types.Card, error) {
	cards := bodies(rows)
	if err := r.attachSchedules(ctx, caller, cards); err != nil {
		return nil, err
	}
	return cards, nil
}

// attachSchedules gives each Card the caller's own Review schedule of it, read
// per request: it is the one thing a Card carries that is not shared data, so no
// entry holds it (ADR-0001). A Card the caller has not studied keeps none.
func (r *Reader) attachSchedules(ctx context.Context, caller int64, cards []*types.Card) error {
	if caller == access.Anonymous || len(cards) == 0 {
		return nil
	}

	cardIDs := make([]int64, 0, len(cards))
	for _, card := range cards {
		cardIDs = append(cardIDs, int64(card.Id))
	}

	reviews, err := r.db.ListUserCardSchedules(ctx, db.ListUserCardSchedulesParams{
		UserID:  caller,
		CardIds: cardIDs,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSchedules, err)
	}

	byCard := make(map[int64]*db.UserCard, len(reviews))
	for i := range reviews {
		byCard[reviews[i].CardID] = &reviews[i]
	}

	for _, card := range cards {
		if review, ok := byCard[int64(card.Id)]; ok {
			card.ReviewSchedule = scheduleOf(review)
		}
	}
	return nil
}

// Invalidate drops every entry holding the Deck's Cards, the windows they were
// read in included.
func (r *Reader) Invalidate(ctx context.Context, deckID int64) {
	if r.cache == nil {
		return
	}
	_ = r.cache.DeletePrefix(ctx, deckKey(deckID))
}

// bodyOf is the one row→wire mapping: a Card as callers see it, carrying no
// Review schedule until the caller's own is attached.
func bodyOf(card db.Card) *types.Card {
	return &types.Card{
		Id:          uint32(card.ID),
		DeckId:      uint32(card.DeckID),
		KoreanWord:  card.KoreanWord,
		EnglishWord: card.EnglishWord,
		Context:     card.Context.String,
		Example:     card.ExampleSentence.String,
	}
}

// bodies renders rows as the wire carries them. It is the same mapping a listing
// gets, so a new Card field is remembered in one place.
func bodies(rows []db.Card) []*types.Card {
	cards := make([]*types.Card, 0, len(rows))
	for _, row := range rows {
		cards = append(cards, bodyOf(row))
	}
	return cards
}

// scheduleOf is a stored Review as the wire carries it.
func scheduleOf(review *db.UserCard) *types.ReviewSchedule {
	return &types.ReviewSchedule{
		Interval:     review.Interval,
		Efactor:      review.Efactor,
		Repetitions:  review.Repetitions,
		NextReviewAt: formatTime(review.NextReviewAt),
	}
}

// formatTime renders a stored moment the way the wire carries it, and a NULL as
// empty.
func formatTime(at pgtype.Timestamptz) string {
	if !at.Valid {
		return ""
	}
	return at.Time.UTC().Format(time.RFC3339)
}

// callerParam carries a caller id into a query that filters by access. An
// anonymous caller is NULL, which matches no owner and no DeckShare.
func callerParam(caller int64) pgtype.Int8 {
	return pgtype.Int8{Int64: caller, Valid: caller != access.Anonymous}
}
