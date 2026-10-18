package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

const profileRoute = "/api/v1/user/profile"

// A profile update is reflected on the next read rather than at the entry's
// expiry.
func TestUpdateUserProfileIsVisibleOnTheNextRead(t *testing.T) {
	h := newTestHandlerWithCache(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	if profile := readProfile(t, h, learner); profile.Name != "learner@example.com" {
		t.Fatalf("expected the seeded profile, got %+v", profile)
	}

	w := put(t, h.UpdateUserProfile, learner, profileRoute, profileRoute,
		`{"name":"Renamed","email":"renamed@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to update the profile: %d: %s", w.Code, w.Body.String())
	}

	profile := readProfile(t, h, learner)
	if profile.Name != "Renamed" || profile.Email != "renamed@example.com" {
		t.Fatalf("expected the updated profile, got %+v", profile)
	}
}

// An address another learner holds is refused as a conflict.
func TestUpdateUserProfileRefusesADuplicateEmail(t *testing.T) {
	h := newTestHandler(t)
	dbtest.SeedUser(t, "taken@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")

	w := put(t, h.UpdateUserProfile, learner, profileRoute, profileRoute,
		`{"name":"Learner","email":"taken@example.com"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
	}
}

// A profile edit leaves the password alone: the learner can still sign in with
// the address they changed to and the password they already had.
func TestUpdateUserProfileKeepsSignInWorking(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	w := post(t, h.Register, access.Anonymous, "/api/v1/register", "/api/v1/register", registerBody("jane@example.com"))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to register: %d: %s", w.Code, w.Body.String())
	}

	user, err := h.DB.GetUserByEmail(ctx, "jane@example.com")
	if err != nil {
		t.Fatalf("failed to read the registered learner: %v", err)
	}

	w = put(t, h.UpdateUserProfile, user.ID, profileRoute, profileRoute,
		`{"name":"Jane Doe","email":"jane.doe@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to update the profile: %d: %s", w.Code, w.Body.String())
	}

	w = post(t, h.Login, access.Anonymous, "/api/v1/login", "/api/v1/login",
		`{"email":"jane.doe@example.com","password":"password123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected the learner to sign in after a profile edit, got %d: %s", w.Code, w.Body.String())
	}
}

// Verifying an email changes the badge the next profile read reports, not the
// one a cached profile still holds.
func TestGetUserProfileAfterVerificationReportsVerified(t *testing.T) {
	h := newTestHandlerWithCache(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	delivery := newFakeMail()
	h.Mail = delivery

	if profile := readProfile(t, h, learner); profile.IsVerified {
		t.Fatal("expected the seeded learner to be unverified")
	}

	w := post(t, h.ResendVerificationEmail, learner, "/api/v1/resend-verification", "/api/v1/resend-verification", "")
	if w.Code != http.StatusOK {
		t.Fatalf("failed to resend the verification email: %d: %s", w.Code, w.Body.String())
	}
	if len(delivery.verifications) != 1 {
		t.Fatalf("delivery was attempted %d times, want 1", len(delivery.verifications))
	}

	token := delivery.verifications[0].token
	w = post(t, h.VerifyEmail, access.Anonymous, "/api/v1/verification/:token", "/api/v1/verification/"+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("failed to verify the email: %d: %s", w.Code, w.Body.String())
	}

	if profile := readProfile(t, h, learner); !profile.IsVerified {
		t.Fatal("expected the next profile read to report verified")
	}
}

// readProfile reads the profile through the route and returns what the caller
// saw.
func readProfile(t *testing.T, h *handlers.Handler, caller int64) *types.UserResponse {
	t.Helper()

	w := get(t, h.GetUserProfile, caller, profileRoute, profileRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the profile: %d: %s", w.Code, w.Body.String())
	}

	var profile types.UserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatalf("failed to parse the profile: %v", err)
	}
	return &profile
}
