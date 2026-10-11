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
)

func TestGetSharedDeckByTokenCacheHit(t *testing.T) {
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	_ = c.SetProto(ctx, "deck:shared:tok_12345", &types.Deck{
		Id:         88,
		Name:       "TOPIK II Advanced Vocab",
		Type:       "vocabulary",
		IsPublic:   false,
		ShareToken: "tok_12345",
	}, 5*time.Minute)

	h := handlers.NewHandler(nil, c)
	r := setupTestRouter()
	r.GET("/api/v1/decks/shared/:token", h.GetSharedDeckByToken)

	req := httptest.NewRequest("GET", "/api/v1/decks/shared/tok_12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var result types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if result.Id != 88 || result.Name != "TOPIK II Advanced Vocab" || result.ShareToken != "tok_12345" {
		t.Fatalf("unexpected shared deck response: %+v", result)
	}
}

func TestDeleteAccountUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.DELETE("/api/v1/user", h.DeleteAccount)

	req := httptest.NewRequest("DELETE", "/api/v1/user", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShareDeckUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.POST("/api/v1/decks/:id/share", h.ShareDeck)

	req := httptest.NewRequest("POST", "/api/v1/decks/1/share", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoinSharedDeckUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.POST("/api/v1/decks/shared/:token/join", h.JoinSharedDeck)

	req := httptest.NewRequest("POST", "/api/v1/decks/shared/tok_123/join", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}
