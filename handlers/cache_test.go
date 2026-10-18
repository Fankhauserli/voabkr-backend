package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const settingsRoute = "/api/v1/user/settings"

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("userSession", store))
	return r
}

// An anonymous caller's listing is answered from the entry its first request wrote: a rename
// that went around the handler is not served until a write clears the entry.
func TestGetDecksCacheHit(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	if names := deckNames(t, h, access.Anonymous); len(names) != 1 || names[0] != "Seeded Deck" {
		t.Fatalf("expected the seeded Deck in the listing, got %v", names)
	}

	renameDeckDirectly(t, h.DB, deckID, "Renamed")

	if names := deckNames(t, h, access.Anonymous); len(names) != 1 || names[0] != "Seeded Deck" {
		t.Fatalf("expected the entry to answer the second listing, got %v", names)
	}
}

// The settings a read answers with are the entry its previous update wrote: a
// change that went around the handler is not served until the entry is replaced.
func TestGetSettingsCacheHit(t *testing.T) {
	h := newTestHandlerWithCache(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	w := put(t, h.UpdateSettings, learner, settingsRoute, settingsRoute,
		`{"cardsPerDay":40,"studyDirection":"englishToKorean","scratchPadEnabled":true}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("failed to update the settings: %d: %s", w.Code, w.Body.String())
	}

	updateSettingsDirectly(t, h.DB, learner, 10, "koreanToEnglish", false)

	w = get(t, h.GetSettings, learner, settingsRoute, settingsRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result types.SettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if result.CardsPerDay != 40 || result.StudyDirection != "englishToKorean" || !result.ScratchPadEnabled {
		t.Fatalf("unexpected settings: cardsPerDay=%d studyDirection=%q scratchPadEnabled=%v",
			result.CardsPerDay, result.StudyDirection, result.ScratchPadEnabled)
	}
}

// renameDeckDirectly renames the Deck through the database, leaving the cache
// alone — a write the handler did not announce.
func renameDeckDirectly(t *testing.T, queries *db.Queries, deckID int64, name string) {
	t.Helper()

	ctx := context.Background()
	deck, err := queries.GetDeck(ctx, deckID)
	if err != nil {
		t.Fatalf("failed to read the Deck to rename: %v", err)
	}
	if err := queries.UpdateDeck(ctx, db.UpdateDeckParams{
		ID:       deck.ID,
		Name:     name,
		Type:     deck.Type,
		IsPublic: deck.IsPublic,
	}); err != nil {
		t.Fatalf("failed to rename the Deck: %v", err)
	}
}

// renameCardDirectly changes the Card's words through the database, leaving the
// cache alone — a write the handler did not announce.
func renameCardDirectly(t *testing.T, queries *db.Queries, cardID, deckID int64, korean, english string) {
	t.Helper()

	err := queries.UpdateCard(context.Background(), db.UpdateCardParams{
		ID:          cardID,
		DeckID:      deckID,
		KoreanWord:  korean,
		EnglishWord: english,
	})
	if err != nil {
		t.Fatalf("failed to rename the Card: %v", err)
	}
}

// updateSettingsDirectly changes the stored settings, leaving the cache alone —
// a write the handler did not announce.
func updateSettingsDirectly(t *testing.T, queries *db.Queries, learner int64, cardsPerDay int32, studyDirection string, scratchPadEnabled bool) {
	t.Helper()

	err := queries.UpdateUserSettings(context.Background(), db.UpdateUserSettingsParams{
		UserID:            learner,
		CardsPerDay:       cardsPerDay,
		StudyDirection:    studyDirection,
		ScratchPadEnabled: scratchPadEnabled,
	})
	if err != nil {
		t.Fatalf("failed to update the settings: %v", err)
	}
}
