// Package reviews owns the Review queue and every Grade write. The queue is the
// Cards a learner is due to study, optionally within one Deck or since a given
// moment; the caller asks for it rather than assembling it from Card rows, and
// the Cards arrive carrying their own Review schedules.
//
// Which Cards a queue holds is decided in SQL: the due and since reads select
// from the one readable-Decks rule, so a Deck the caller may not read — deleted
// included — takes its Cards out of the queue. The optional Deck filter is part
// of that read, not a pass over its answer. Every Grade write asks the same
// readable-Deck gate and answers in typed outcomes.
package reviews

import (
	"context"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5/pgtype"
)

// Reader answers the Review queue for the learner asking.
type Reader struct {
	db    *db.Queries
	cards *cards.Reader
}

// NewReader returns a Reader over the database the queue is read from, with the
// Card reader that gives each Card the caller's own Review schedule.
func NewReader(q *db.Queries, c *cards.Reader) *Reader {
	return &Reader{db: q, cards: c}
}

// Query is what a queue read asks. Caller is whose Reviews are read. DeckID is
// the optional Deck filter: zero means every Deck the caller may read. Since is
// the optional moment the queue is read from: zero means "due now".
type Query struct {
	Caller int64
	DeckID int64
	Since  time.Time
}

// Queue returns the caller's queued Cards, each carrying their own Review
// schedule. The queue is unpaged: a study session is offered what is due, and
// the order it offers them in is the device's.
func (r *Reader) Queue(ctx context.Context, query Query) ([]*types.Card, error) {
	var (
		rows []db.Card
		err  error
	)
	if query.Since.IsZero() {
		rows, err = r.db.GetCardsDueForReview(ctx, db.GetCardsDueForReviewParams{
			UserID: query.Caller,
			DeckID: query.DeckID,
		})
	} else {
		rows, err = r.db.GetCardsDueForReviewAfter(ctx, db.GetCardsDueForReviewAfterParams{
			UserID:       query.Caller,
			NextReviewAt: pgtype.Timestamptz{Time: query.Since, Valid: true},
			DeckID:       query.DeckID,
		})
	}
	if err != nil {
		return nil, err
	}

	return r.cards.FromRows(ctx, query.Caller, rows)
}
