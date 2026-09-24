package types

type Card struct {
	ID          uint   `json:"id"`
	DeckID      uint   `json:"deckId"`
	KoreanWord  string `json:"koreanWord"`
	EnglishWord string `json:"englishWord"`
	Context     string `json:"context"`
	Example     string `json:"example"`
}

type CardRequest struct {
	DeckID      uint   `json:"deckId"`
	KoreanWord  string `json:"koreanWord"`
	EnglishWord string `json:"englishWord"`
	Context     string `json:"context"`
	Example     string `json:"example"`
}
