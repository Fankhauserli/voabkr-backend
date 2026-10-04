package types_test

import (
	"encoding/json"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/go-playground/validator/v10"
	"google.golang.org/protobuf/proto"
)

func TestAuthJSON(t *testing.T) {
	loginJSON := `{"email":"test@example.com","password":"secretpassword"}`
	var loginReq types.LoginRequestBody
	if err := json.Unmarshal([]byte(loginJSON), &loginReq); err != nil {
		t.Fatalf("failed to unmarshal LoginRequestBody: %v", err)
	}
	if loginReq.GetEmail() != "test@example.com" || loginReq.GetPassword() != "secretpassword" {
		t.Fatalf("unexpected values: %+v", loginReq)
	}

	validate := validator.New()
	validate.SetTagName("binding")
	if err := validate.Struct(&loginReq); err != nil {
		t.Fatalf("valid LoginRequestBody failed validation: %v", err)
	}

	badLogin := types.LoginRequestBody{Email: "not-an-email", Password: ""}
	if err := validate.Struct(&badLogin); err == nil {
		t.Fatalf("expected validation failure for invalid email and empty password")
	}

	regJSON := `{"email":"new@example.com","password":"secretpassword","name":"Tester"}`
	var regReq types.RegisterRequestBody
	if err := json.Unmarshal([]byte(regJSON), &regReq); err != nil {
		t.Fatalf("failed to unmarshal RegisterRequestBody: %v", err)
	}
	if regReq.GetName() != "Tester" || regReq.GetEmail() != "new@example.com" {
		t.Fatalf("unexpected values: %+v", regReq)
	}
	if err := validate.Struct(&regReq); err != nil {
		t.Fatalf("valid RegisterRequestBody failed validation: %v", err)
	}

	resendJSON := `{"email":"resend@example.com"}`
	var resendReq types.ResendVerificationRequest
	if err := json.Unmarshal([]byte(resendJSON), &resendReq); err != nil {
		t.Fatalf("failed to unmarshal ResendVerificationRequest: %v", err)
	}
	if resendReq.GetEmail() != "resend@example.com" {
		t.Fatalf("unexpected values: %+v", resendReq)
	}
}

func TestCardJSON(t *testing.T) {
	card := &types.Card{
		Id:          10,
		DeckId:      2,
		KoreanWord:  "사과",
		EnglishWord: "apple",
		Context:     "과일",
		Example:     "사과를 먹었습니다.",
	}

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("failed to marshal Card: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal json map: %v", err)
	}

	expectedKeys := []string{"id", "deckId", "koreanWord", "englishWord", "context", "example"}
	for _, k := range expectedKeys {
		if _, ok := jsonMap[k]; !ok {
			t.Errorf("missing expected key '%s' in Card JSON: %s", k, string(data))
		}
	}

	var decoded types.Card
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Card: %v", err)
	}
	if !proto.Equal(card, &decoded) {
		t.Errorf("decoded Card does not match original: got %+v, want %+v", decoded, card)
	}

	cardReqJSON := `{"deckId":5,"koreanWord":"물","englishWord":"water","context":"음료","example":"물을 마십니다."}`
	var cardReq types.CardRequest
	if err := json.Unmarshal([]byte(cardReqJSON), &cardReq); err != nil {
		t.Fatalf("failed to unmarshal CardRequest: %v", err)
	}
	if cardReq.GetDeckId() != 5 || cardReq.GetKoreanWord() != "물" {
		t.Fatalf("unexpected CardRequest: %+v", cardReq)
	}
}

func TestDeckJSON(t *testing.T) {
	deck := &types.Deck{
		Id:   1,
		Name: "JLPT N5",
		Type: "vocabulary",
	}

	data, err := json.Marshal(deck)
	if err != nil {
		t.Fatalf("failed to marshal Deck: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal json map: %v", err)
	}

	for _, k := range []string{"id", "name", "type"} {
		if _, ok := jsonMap[k]; !ok {
			t.Errorf("missing key '%s' in Deck JSON: %s", k, string(data))
		}
	}

	deckReqJSON := `{"name":"Basic Grammar","type":"grammar"}`
	var deckReq types.DeckRequest
	if err := json.Unmarshal([]byte(deckReqJSON), &deckReq); err != nil {
		t.Fatalf("failed to unmarshal DeckRequest: %v", err)
	}
	if deckReq.GetName() != "Basic Grammar" || deckReq.GetType() != "grammar" {
		t.Fatalf("unexpected DeckRequest: %+v", deckReq)
	}
}

func TestReviewJSON(t *testing.T) {
	reviewJSON := `{"cardID":42}`
	var review types.Review
	if err := json.Unmarshal([]byte(reviewJSON), &review); err != nil {
		t.Fatalf("failed to unmarshal Review: %v", err)
	}
	if review.GetCardId() != 42 {
		t.Fatalf("expected cardID 42, got %d", review.GetCardId())
	}

	revReqJSON := `{"ease":3}`
	var revReq types.ReviewRequest
	if err := json.Unmarshal([]byte(revReqJSON), &revReq); err != nil {
		t.Fatalf("failed to unmarshal ReviewRequest: %v", err)
	}
	if revReq.GetEase() != 3 {
		t.Fatalf("expected ease 3, got %d", revReq.GetEase())
	}
}

func TestSettingsJSON(t *testing.T) {
	settings := &types.SettingsResponse{
		CardsPerDay:       25,
		StudyDirection:    "koreanToEnglish",
		ScratchPadEnabled: true,
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal SettingsResponse: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal json map: %v", err)
	}

	for _, k := range []string{"cardsPerDay", "studyDirection", "scratchPadEnabled"} {
		if _, ok := jsonMap[k]; !ok {
			t.Errorf("missing key '%s' in SettingsResponse JSON: %s", k, string(data))
		}
	}

	// Test partial update
	updateJSON := `{"cardsPerDay":50}`
	var updateReq types.UpdateSettingsRequest
	if err := json.Unmarshal([]byte(updateJSON), &updateReq); err != nil {
		t.Fatalf("failed to unmarshal UpdateSettingsRequest: %v", err)
	}
	if updateReq.CardsPerDay == nil || *updateReq.CardsPerDay != 50 {
		t.Fatalf("expected CardsPerDay to be 50, got %v", updateReq.CardsPerDay)
	}
	if updateReq.StudyDirection != nil {
		t.Fatalf("expected StudyDirection to be nil, got %v", updateReq.StudyDirection)
	}
	if updateReq.ScratchPadEnabled != nil {
		t.Fatalf("expected ScratchPadEnabled to be nil, got %v", updateReq.ScratchPadEnabled)
	}
}

func TestUserJSON(t *testing.T) {
	userResp := &types.UserResponse{
		Id:         1,
		Name:       "Jane Doe",
		Email:      "jane@example.com",
		IsActive:   true,
		IsVerified: false,
	}

	data, err := json.Marshal(userResp)
	if err != nil {
		t.Fatalf("failed to marshal UserResponse: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal json map: %v", err)
	}

	for _, k := range []string{"id", "name", "email", "isActive", "isVerified"} {
		if _, ok := jsonMap[k]; !ok {
			t.Errorf("missing key '%s' in UserResponse JSON: %s", k, string(data))
		}
	}

	// Verify false values are emitted (emit_defaults=true)
	if val, ok := jsonMap["isVerified"]; !ok || val != false {
		t.Fatalf("expected isVerified to be false, got %v", val)
	}

	userReqJSON := `{"name":"New Name","email":"new@example.com"}`
	var userReq types.UserRequest
	if err := json.Unmarshal([]byte(userReqJSON), &userReq); err != nil {
		t.Fatalf("failed to unmarshal UserRequest: %v", err)
	}
	if userReq.GetName() != "New Name" || userReq.GetEmail() != "new@example.com" {
		t.Fatalf("unexpected UserRequest: %+v", userReq)
	}
}
