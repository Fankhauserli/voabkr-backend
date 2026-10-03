package types

type SettingsResponse struct {
	CardsPerDay       uint   `json:"cardsPerDay"`
	StudyDirection    string `json:"studyDirection"`
	ScratchPadEnabled bool   `json:"scratchPadEnabled"`
}

type UpdateSettingsRequest struct {
	CardsPerDay       *uint   `json:"cardsPerDay"`
	StudyDirection    *string `json:"studyDirection"`
	ScratchPadEnabled *bool   `json:"scratchPadEnabled"`
}
