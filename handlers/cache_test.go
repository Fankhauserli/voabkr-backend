package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("userSession", store))
	return r
}

func TestGetDecksCacheHit(t *testing.T) {
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer c.Close()

	// Pre-populate cache
	cachedDecks := []*types.Deck{
		{Id: 1, Name: "Cached Deck 1", Type: "vocabulary"},
		{Id: 2, Name: "Cached Deck 2", Type: "grammar"},
	}
	ctx := context.Background()
	_ = c.SetProto(ctx, "decks:all", &types.DeckList{Decks: cachedDecks}, 5*time.Minute)

	// Handler with nil DB - proving DB is not touched on cache hit
	h := handlers.NewHandler(nil, c)

	r := setupTestRouter()
	r.GET("/api/v1/decks", h.GetDecks)

	req := httptest.NewRequest("GET", "/api/v1/decks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if len(result) != 2 || result[0].Name != "Cached Deck 1" {
		t.Fatalf("unexpected cached decks response: %+v", result)
	}
}

func TestGetDeckByIDCacheHit(t *testing.T) {
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	_ = c.SetProto(ctx, "deck:42", &types.Deck{Id: 42, Name: "Special Deck", Type: "vocabulary"}, 5*time.Minute)

	h := handlers.NewHandler(nil, c)
	r := setupTestRouter()
	r.GET("/api/v1/decks/:id", h.GetDeckByID)

	req := httptest.NewRequest("GET", "/api/v1/decks/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if result.Id != 42 || result.Name != "Special Deck" {
		t.Fatalf("unexpected cached deck: %+v", result)
	}
}

func TestGetCardsCacheHit(t *testing.T) {
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer c.Close()

	cachedCards := []*types.Card{
		{Id: 101, DeckId: 1, KoreanWord: "나무", EnglishWord: "tree", Context: "자연", Example: "나무가 큽니다."},
	}
	ctx := context.Background()
	_ = c.SetProto(ctx, "cards:all", &types.CardList{Cards: cachedCards}, 5*time.Minute)

	h := handlers.NewHandler(nil, c)
	r := setupTestRouter()
	r.GET("/api/v1/cards", h.GetCards)

	req := httptest.NewRequest("GET", "/api/v1/cards", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []types.Card
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if len(result) != 1 || result[0].KoreanWord != "나무" {
		t.Fatalf("unexpected cached cards response: %+v", result)
	}
}

func TestGetSettingsCacheHit(t *testing.T) {
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	_ = c.SetProto(ctx, "settings:user:7", &types.SettingsResponse{
		CardsPerDay:       40,
		StudyDirection:    "englishToKorean",
		ScratchPadEnabled: true,
	}, 5*time.Minute)

	h := handlers.NewHandler(nil, c)
	r := setupTestRouter()
	r.GET("/api/v1/user/settings", func(ctx *gin.Context) {
		session := sessions.Default(ctx)
		session.Set("user_id", "7")
		_ = session.Save()
		h.GetSettings(ctx)
	})

	req := httptest.NewRequest("GET", "/api/v1/user/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result types.SettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if result.CardsPerDay != 40 || result.StudyDirection != "englishToKorean" || !result.ScratchPadEnabled {
		t.Fatalf("unexpected cached settings: %+v", result)
	}
}
