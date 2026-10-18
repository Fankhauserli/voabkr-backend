package handlers

import (
	"github.com/Fankhauserli/voabkr-backend/account"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/mail"
	"github.com/Fankhauserli/voabkr-backend/reviews"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/tokens"
)

type Handler struct {
	DB *db.Queries

	// Decks reads Decks: which entry answers one, the fetch on miss, and what a
	// write invalidates. No handler holds one of its keys.
	Decks *decks.Reader

	// Cards reads Cards the same way, Review schedules and all.
	Cards *cards.Reader

	// Reviews reads the queue: the Cards a learner is due to study, within one
	// Deck or since a moment at the caller's option.
	Reviews *reviews.Reader

	// Grades writes every Grade: enrolling a Card, storing a device-computed
	// batch, and the legacy server-computed rating. All three ask one gate.
	Grades *reviews.Writer

	// Account owns a learner's profile and settings: their cache entries, the
	// fetch on miss, the defaults, and what a write invalidates. No handler holds
	// one of its keys.
	Account *account.Account

	// Mail delivers account messages. It is an interface so a test can observe
	// the flow with a fake outcome; the real implementation is delivery-only and
	// reads its configuration once, in NewHandler.
	Mail mail.Mailer

	// Tokens mints the opaque tokens share links, verification and password
	// reset carry. No handler reads entropy itself.
	Tokens *tokens.Issuer
}

func NewHandler(db *db.Queries, c cache.Cache) *Handler {
	cardReader := cards.NewReader(db, c)
	deckReader := decks.NewReader(db, c)
	return &Handler{
		DB:      db,
		Decks:   deckReader,
		Cards:   cardReader,
		Reviews: reviews.NewReader(db, cardReader),
		Grades:  reviews.NewWriter(db, deckReader),
		Account: account.New(db, c),
		Mail:    mail.NewFromEnv(),
		Tokens:  tokens.NewIssuer(),
	}
}
