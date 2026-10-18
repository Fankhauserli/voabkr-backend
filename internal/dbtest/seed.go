package dbtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// SeedUser inserts a learner and returns its id.
func SeedUser(t *testing.T, email string) int64 {
	t.Helper()
	user, err := Queries(t).CreateUser(context.Background(), db.CreateUserParams{
		Name:         email,
		Email:        email,
		PasswordHash: "not-a-real-hash",
	})
	if err != nil {
		t.Fatalf("failed to seed learner %s: %v", email, err)
	}
	return user.ID
}

// SeedDeck inserts a Deck named "Seeded Deck". ownerID 0 inserts a System deck;
// every seeded Deck carries a share token, as CreateDeck leaves in production.
func SeedDeck(t *testing.T, ownerID int64, isPublic bool) int64 {
	t.Helper()
	return SeedNamedDeck(t, ownerID, isPublic, "Seeded Deck")
}

// SeedNamedDeck inserts a Deck under the given name.
func SeedNamedDeck(t *testing.T, ownerID int64, isPublic bool, name string) int64 {
	t.Helper()
	deck, err := Queries(t).CreateDeck(context.Background(), db.CreateDeckParams{
		Name:       name,
		Type:       db.DeckTypeVocabulary,
		OwnerID:    pgtype.Int8{Int64: ownerID, Valid: ownerID > 0},
		IsPublic:   isPublic,
		ShareToken: pgtype.Text{String: fmt.Sprintf("sec_seed_%d", seedTokenSeq()), Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to seed deck: %v", err)
	}
	return deck.ID
}

var tokenSeq int

func seedTokenSeq() int {
	tokenSeq++
	return tokenSeq
}

// SeedCard inserts a Card into deckID and returns its id.
func SeedCard(t *testing.T, deckID int64, korean, english string) int64 {
	t.Helper()
	card, err := Queries(t).CreateCard(context.Background(), db.CreateCardParams{
		DeckID:      deckID,
		KoreanWord:  korean,
		EnglishWord: english,
	})
	if err != nil {
		t.Fatalf("failed to seed card: %v", err)
	}
	return card.ID
}

// SeedDueReview gives learner a Review of cardID that fell due an hour ago, as
// one that was studied and left to come round again.
func SeedDueReview(t *testing.T, learner, cardID int64) {
	t.Helper()
	SeedScheduledReview(t, learner, cardID, time.Now().Add(-time.Hour))
}

// SeedScheduledReview gives learner a Review of cardID due at nextReviewAt, as
// one the device scheduled and answered an hour before it.
func SeedScheduledReview(t *testing.T, learner, cardID int64, nextReviewAt time.Time) {
	t.Helper()
	ctx := context.Background()

	if _, err := Queries(t).CreateUserCard(ctx, db.CreateUserCardParams{UserID: learner, CardID: cardID}); err != nil {
		t.Fatalf("failed to seed a review: %v", err)
	}

	err := Queries(t).UpdateUserCard(ctx, db.UpdateUserCardParams{
		UserID:         learner,
		CardID:         cardID,
		Efactor:        2.5,
		Interval:       6,
		Repetitions:    1,
		LastReviewedAt: pgtype.Timestamptz{Time: nextReviewAt.Add(-time.Hour), Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: nextReviewAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to seed a scheduled review: %v", err)
	}
}

// SeedShare grants userID permission on deckID.
func SeedShare(t *testing.T, deckID, userID int64, permission string) {
	t.Helper()
	_, err := Queries(t).CreateDeckShare(context.Background(), db.CreateDeckShareParams{
		DeckID:     deckID,
		UserID:     pgtype.Int8{Int64: userID, Valid: true},
		Permission: permission,
	})
	if err != nil {
		t.Fatalf("failed to seed deck share: %v", err)
	}
}
