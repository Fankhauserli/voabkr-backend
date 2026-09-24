package types

type UserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
type UserResponse struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	IsActive   bool   `json:"isActive"`
	IsVerified bool   `json:"isVerified"`
}
