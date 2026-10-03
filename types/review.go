package types

type Review struct {
	CardID uint `json:"cardID"`
}

type ReviewRequest struct {
	Ease int `json:"ease"`
}
