package types

type Deck struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type DeckRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
