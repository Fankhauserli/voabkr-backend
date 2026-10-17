package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
)

const gradesRoute = "/api/v1/reviews/batch"

// gradeEntry is one Grade as the device sends it: the Card, the Grade given, the
// Review schedule it computed, and the moment the learner answered.
func gradeEntry(cardID int64, grade int, interval int, efactor float64, repetitions int, answeredAt, nextReviewAt time.Time) string {
	return fmt.Sprintf(
		`{"cardId":%d,"grade":%d,"answeredAt":%q,"schedule":{"interval":%d,"efactor":%g,"repetitions":%d,"nextReviewAt":%q}}`,
		cardID, grade, rfc3339(answeredAt), interval, efactor, repetitions, rfc3339(nextReviewAt))
}

func gradeBody(entries ...string) string {
	return `{"grades":[` + strings.Join(entries, ",") + `]}`
}

// rfc3339 renders a moment the way the wire carries it, at the precision Postgres
// keeps.
func rfc3339(at time.Time) string {
	return at.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// A Grade arrives carrying the Review schedule the device computed, and the
// backend stores that schedule rather than computing one of its own.
func TestSubmitGradesStoresTheScheduleTheDeviceComputed(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	answeredAt := time.Now().Add(-time.Hour)
	nextReviewAt := answeredAt.Add(6 * 24 * time.Hour)

	w := post(t, h.SubmitGrades, learner, gradesRoute, gradesRoute,
		gradeBody(gradeEntry(cardID, 3, 6, 2.36, 3, answeredAt, nextReviewAt)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var batch batchResults
	unmarshalBatch(t, w.Body.Bytes(), &batch)
	if len(batch.Results) != 1 || !batch.Results[0].Accepted {
		t.Fatalf("expected one accepted Grade, got %s", w.Body.String())
	}

	// The stored Review is the device's, whole: the Interval, the Efactor, and
	// the repetitions it computed, answered when it says it was.
	review, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: cardID})
	if err != nil {
		t.Fatalf("expected a stored Review, got %v", err)
	}
	if review.Interval != 6 || review.Efactor != 2.36 || review.Repetitions != 3 {
		t.Fatalf("expected the device's schedule 6/2.36/3, got %d/%g/%d", review.Interval, review.Efactor, review.Repetitions)
	}
	if !review.LastReviewedAt.Valid || !review.LastReviewedAt.Time.Equal(answeredAt.Truncate(time.Second)) {
		t.Fatalf("expected last_reviewed_at %s, got %v", rfc3339(answeredAt), review.LastReviewedAt)
	}
	if !review.NextReviewAt.Valid || !review.NextReviewAt.Time.Equal(nextReviewAt.Truncate(time.Second)) {
		t.Fatalf("expected next_review_at %s, got %v", rfc3339(nextReviewAt), review.NextReviewAt)
	}
}

// One refused Grade is one entry's problem: the batch still reports a result per
// entry, and the Grades it could take are stored.
func TestSubmitGradesRefusesOneEntryWithoutFailingTheBatch(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")

	ownDeck := dbtest.SeedDeck(t, learner, false)
	ownCard := dbtest.SeedCard(t, ownDeck, "내것", "mine")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	strangerCard := dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")

	answeredAt := time.Now().Add(-time.Hour)
	nextReviewAt := answeredAt.Add(24 * time.Hour)

	w := post(t, h.SubmitGrades, learner, gradesRoute, gradesRoute, gradeBody(
		gradeEntry(ownCard, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
		gradeEntry(9999, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
		gradeEntry(strangerCard, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
	))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for a batch carrying a refused Grade, got %d: %s", w.Code, w.Body.String())
	}

	var batch batchResults
	unmarshalBatch(t, w.Body.Bytes(), &batch)
	if len(batch.Results) != 3 {
		t.Fatalf("expected a result per entry, got %s", w.Body.String())
	}

	if !batch.Results[0].Accepted {
		t.Fatalf("expected the learner's own Card to be accepted, got %+v", batch.Results[0])
	}
	if batch.Results[1].Accepted || batch.Results[1].Reason == "" {
		t.Fatalf("expected an unknown Card to be refused with a reason, got %+v", batch.Results[1])
	}
	if batch.Results[2].Accepted || batch.Results[2].Reason == "" {
		t.Fatalf("expected a Card the learner may not read to be refused with a reason, got %+v", batch.Results[2])
	}

	// The accepted Grade is stored; the refused Grade for a Deck the learner may
	// not read stored nothing at all.
	if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: ownCard}); err != nil {
		t.Fatalf("expected the accepted Grade to be stored, got %v", err)
	}
	if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: strangerCard}); err == nil {
		t.Fatalf("expected no Review stored for a Deck the learner may not read")
	}
}

// A replayed Grade is a no-op, and between two answers the later one wins.
func TestSubmitGradesReplayIsANoOpAndTheLaterAnswerWins(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	first := time.Now().Add(-2 * time.Hour)
	later := first.Add(time.Hour)

	submit := func(grade, interval int, efactor float64, answeredAt, nextReviewAt time.Time) batchResults {
		t.Helper()
		w := post(t, h.SubmitGrades, learner, gradesRoute, gradesRoute,
			gradeBody(gradeEntry(cardID, grade, interval, efactor, 1, answeredAt, nextReviewAt)))
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
		var batch batchResults
		unmarshalBatch(t, w.Body.Bytes(), &batch)
		return batch
	}

	stored := func() db.UserCard {
		t.Helper()
		review, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: cardID})
		if err != nil {
			t.Fatalf("expected a stored Review, got %v", err)
		}
		return review
	}

	submit(3, 6, 2.36, first, first.Add(6*24*time.Hour))

	// A Sync that sends the same Grade again is accepted — the device has nothing
	// left to do with it — and changes nothing.
	if got := submit(3, 6, 2.36, first, first.Add(6*24*time.Hour)); !got.Results[0].Accepted {
		t.Fatalf("expected a replayed Grade to be accepted, got %+v", got.Results[0])
	}
	if review := stored(); review.Interval != 6 || review.Efactor != 2.36 {
		t.Fatalf("expected the replay to leave the Review at 6/2.36, got %d/%g", review.Interval, review.Efactor)
	}

	// An answer given before the stored one cannot undo it.
	if got := submit(1, 0, 1.3, first.Add(-time.Minute), first.Add(time.Minute)); !got.Results[0].Accepted {
		t.Fatalf("expected a superseded Grade to be accepted, got %+v", got.Results[0])
	}
	if review := stored(); review.Interval != 6 || review.Efactor != 2.36 ||
		!review.LastReviewedAt.Time.Equal(first.Truncate(time.Second)) {
		t.Fatalf("expected the later answer to stand, got %d/%g answered %v", review.Interval, review.Efactor, review.LastReviewedAt)
	}

	// An answer given later takes its place.
	submit(2, 3, 2.2, later, later.Add(3*24*time.Hour))
	if review := stored(); review.Interval != 3 || review.Efactor != 2.2 ||
		!review.LastReviewedAt.Time.Equal(later.Truncate(time.Second)) {
		t.Fatalf("expected the later answer to win, got %d/%g answered %v", review.Interval, review.Efactor, review.LastReviewedAt)
	}
}

// A batch that carries no Grades is refused rather than answered: a device would
// take a 200 as the batch delivered and drop the Grades it meant to send, which
// is how a mistyped key arrives — unknown fields are discarded on the way in.
func TestSubmitGradesRefusesAnEmptyBatch(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	w := post(t, h.SubmitGrades, learner, gradesRoute, gradesRoute, `{"Grades":[]}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for a batch with no Grades, got %d: %s", w.Code, w.Body.String())
	}
}

// The queue answers from the stored schedule: a Review due at or before now is
// in it, one due later and one never studied are not.
func TestGetReviewQueueAnswersFromTheStoredSchedule(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	overdue := dbtest.SeedCard(t, deckID, "지난", "overdue")
	later := dbtest.SeedCard(t, deckID, "나중", "not due yet")
	dbtest.SeedCard(t, deckID, "새것", "never studied")

	dbtest.SeedScheduledReview(t, learner, overdue, time.Now().Add(-2*time.Hour))
	dbtest.SeedScheduledReview(t, learner, later, time.Now().Add(2*time.Hour))

	w := get(t, h.GetReviews, learner, reviewsRoute, "/api/v1/reviews/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 1 || got[0] != "지난" {
		t.Fatalf("expected the overdue Card alone, got %v", got)
	}
}

// Card responses carry the Review schedule, so a device can compute forward from
// what it is given; a Card the learner has not studied carries none.
func TestCardResponsesCarryTheReviewSchedule(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	studied := dbtest.SeedCard(t, deckID, "배운", "studied")
	unstudied := dbtest.SeedCard(t, deckID, "새것", "never studied")

	nextReviewAt := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, learner, studied, nextReviewAt)

	w := get(t, h.GetCards, learner, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var cards []cardSchedule
	if err := json.Unmarshal(w.Body.Bytes(), &cards); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	for _, card := range cards {
		switch int64(card.Id) {
		case studied:
			if card.ReviewSchedule == nil {
				t.Fatalf("expected the studied Card to carry its Review schedule, got %s", w.Body.String())
			}
			if card.ReviewSchedule.Interval != 6 || card.ReviewSchedule.Efactor != 2.5 || card.ReviewSchedule.Repetitions != 1 {
				t.Fatalf("expected the stored schedule 6/2.5/1, got %+v", card.ReviewSchedule)
			}
			if card.ReviewSchedule.NextReviewAt != rfc3339(nextReviewAt) {
				t.Fatalf("expected nextReviewAt %s, got %q", rfc3339(nextReviewAt), card.ReviewSchedule.NextReviewAt)
			}
		case unstudied:
			if card.ReviewSchedule != nil {
				t.Fatalf("expected an unstudied Card to carry no Review schedule, got %+v", card.ReviewSchedule)
			}
		default:
			t.Fatalf("unexpected Card %d in the listing", card.Id)
		}
	}
}

// The wire shapes these tests read, named here so a rename of the response fields
// fails in one place rather than in every assertion.
type batchResults struct {
	Results []struct {
		CardId   uint32 `json:"cardId"`
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	} `json:"results"`
}

type cardSchedule struct {
	Id             uint32 `json:"id"`
	KoreanWord     string `json:"koreanWord"`
	ReviewSchedule *struct {
		Interval     int32   `json:"interval"`
		Efactor      float64 `json:"efactor"`
		Repetitions  int32   `json:"repetitions"`
		NextReviewAt string  `json:"nextReviewAt"`
	} `json:"reviewSchedule"`
}

func unmarshalBatch(t *testing.T, body []byte, into *batchResults) {
	t.Helper()
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
}
