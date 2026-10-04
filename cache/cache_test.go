package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/types"
	"google.golang.org/protobuf/proto"
)

func TestLocalMemoryCache(t *testing.T) {
	mem := cache.NewMemoryCache(100 * time.Millisecond)
	defer mem.Close()

	key := "test:key:1"
	val := []byte("hello-valkey")

	mem.Set(key, val, 1*time.Second)

	ret, found := mem.Get(key)
	if !found {
		t.Fatalf("expected key %s to be found", key)
	}
	if string(ret) != string(val) {
		t.Fatalf("expected %s, got %s", val, ret)
	}

	// Test expiration
	mem.Set("expiring:key", []byte("expire-soon"), 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, found = mem.Get("expiring:key")
	if found {
		t.Fatalf("expected expiring:key to be expired")
	}

	// Test DeletePrefix
	mem.Set("cards:1", []byte("card1"), 1*time.Minute)
	mem.Set("cards:2", []byte("card2"), 1*time.Minute)
	mem.Set("decks:1", []byte("deck1"), 1*time.Minute)

	mem.DeletePrefix("cards:")

	if _, found := mem.Get("cards:1"); found {
		t.Errorf("expected cards:1 to be deleted")
	}
	if _, found := mem.Get("cards:2"); found {
		t.Errorf("expected cards:2 to be deleted")
	}
	if _, found := mem.Get("decks:1"); !found {
		t.Errorf("expected decks:1 to remain")
	}
}

func TestTwoLevelCacheWithProtobuf(t *testing.T) {
	ctx := context.Background()
	c, err := cache.NewTwoLevelCache(cache.Config{
		EnableLocalCache: true,
		DefaultTTL:       1 * time.Minute,
		LocalCacheTTL:    30 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create TwoLevelCache: %v", err)
	}
	defer c.Close()

	// Test single proto message (Deck)
	deck := &types.Deck{
		Id:   10,
		Name: "Grammar Master",
		Type: "grammar",
	}

	deckKey := "deck:10"
	if err := c.SetProto(ctx, deckKey, deck, 1*time.Minute); err != nil {
		t.Fatalf("failed to SetProto: %v", err)
	}

	var retrievedDeck types.Deck
	found, err := c.GetProto(ctx, deckKey, &retrievedDeck)
	if err != nil {
		t.Fatalf("GetProto error: %v", err)
	}
	if !found {
		t.Fatalf("expected deck to be found in cache")
	}
	if !proto.Equal(deck, &retrievedDeck) {
		t.Errorf("retrieved deck mismatch: got %+v, want %+v", retrievedDeck, deck)
	}

	// Test list proto message (CardList)
	cardList := &types.CardList{
		Cards: []*types.Card{
			{
				Id:          1,
				DeckId:      10,
				KoreanWord:  "한국어",
				EnglishWord: "Korean",
				Context:     "Language",
				Example:     "한국어를 배웁니다.",
			},
			{
				Id:          2,
				DeckId:      10,
				KoreanWord:  "영어",
				EnglishWord: "English",
				Context:     "Language",
				Example:     "영어를 공부합니다.",
			},
		},
	}

	cardListKey := "cards:deck:10"
	if err := c.SetProto(ctx, cardListKey, cardList, 1*time.Minute); err != nil {
		t.Fatalf("failed to SetProto for CardList: %v", err)
	}

	var retrievedList types.CardList
	found, err = c.GetProto(ctx, cardListKey, &retrievedList)
	if err != nil {
		t.Fatalf("GetProto error: %v", err)
	}
	if !found {
		t.Fatalf("expected CardList to be found in cache")
	}
	if !proto.Equal(cardList, &retrievedList) {
		t.Errorf("retrieved CardList mismatch: got %+v, want %+v", retrievedList, cardList)
	}

	// Test DeletePrefix
	if err := c.DeletePrefix(ctx, "cards:"); err != nil {
		t.Fatalf("DeletePrefix error: %v", err)
	}

	var afterDelete types.CardList
	found, _ = c.GetProto(ctx, cardListKey, &afterDelete)
	if found {
		t.Errorf("expected CardList to be invalidated after DeletePrefix")
	}

	// Deck should still exist
	found, _ = c.GetProto(ctx, deckKey, &retrievedDeck)
	if !found {
		t.Errorf("expected deck to still exist in cache")
	}
}
