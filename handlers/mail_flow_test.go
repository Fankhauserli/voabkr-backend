package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/helpers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/mail"
)

type mailCall struct {
	to        string
	name      string
	token     string
	expiresIn time.Duration
}

// fakeMail stands in for delivery at the handler seam, so a registration or
// reset flow can be observed without SMTP.
type fakeMail struct {
	verifications []mailCall
	resets        []mailCall

	verificationResult mail.Result
	resetResult        mail.Result
}

func (f *fakeMail) SendVerification(to, name, token string, expiresIn time.Duration) mail.Result {
	f.verifications = append(f.verifications, mailCall{to: to, name: name, token: token, expiresIn: expiresIn})
	return f.verificationResult
}

func (f *fakeMail) SendPasswordReset(to, name, token string, expiresIn time.Duration) mail.Result {
	f.resets = append(f.resets, mailCall{to: to, name: name, token: token, expiresIn: expiresIn})
	return f.resetResult
}

// newFakeMail delivers both messages unless a test says otherwise, so a fake
// never stands in with an outcome the real service cannot produce.
func newFakeMail() *fakeMail {
	return &fakeMail{
		verificationResult: mail.Result{Outcome: mail.Delivered},
		resetResult:        mail.Result{Outcome: mail.Delivered},
	}
}

func registerBody(email string) string {
	return `{"name":"Jane","email":"` + email + `","password":"password123"}`
}

// Registration succeeds once the account exists. A mail outage must not erase
// the learner; resend verification is the recovery path.
func TestRegistrationSucceedsWhateverTheDeliveryOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome mail.Outcome
		err     error
	}{
		{"delivered", mail.Delivered, nil},
		{"skipped", mail.Skipped, nil},
		{"failed", mail.Failed, errors.New("smtp down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			delivery := &fakeMail{verificationResult: mail.Result{Outcome: tt.outcome, Err: tt.err}}
			h.Mail = delivery

			w := post(t, h.Register, access.Anonymous, "/api/v1/register", "/api/v1/register", registerBody("jane@example.com"))

			if w.Code != http.StatusOK {
				t.Fatalf("Register answered %d with %s delivery: %s", w.Code, tt.outcome, w.Body.String())
			}
			if _, err := h.DB.GetUserByEmail(context.Background(), "jane@example.com"); err != nil {
				t.Fatalf("registration did not keep the account: %v", err)
			}
			if len(delivery.verifications) != 1 {
				t.Fatalf("delivery was attempted %d times, want 1", len(delivery.verifications))
			}
		})
	}
}

// The token the mail module is asked to send is the token stored for the
// learner, so the link a delivered message carries actually verifies.
func TestRegistrationIssuesAVerificationTokenThatVerifiesTheEmail(t *testing.T) {
	h := newTestHandler(t)
	delivery := newFakeMail()
	h.Mail = delivery

	w := post(t, h.Register, access.Anonymous, "/api/v1/register", "/api/v1/register", registerBody("jane@example.com"))
	if w.Code != http.StatusOK {
		t.Fatalf("Register answered %d: %s", w.Code, w.Body.String())
	}
	if len(delivery.verifications) != 1 {
		t.Fatalf("delivery was attempted %d times, want 1", len(delivery.verifications))
	}

	if got := delivery.verifications[0].expiresIn; got != 24*time.Hour {
		t.Fatalf("verification email states a lifetime of %v, want 24h", got)
	}

	token := delivery.verifications[0].token
	if len(token) != 128 {
		t.Fatalf("verification token carries %d hex characters, want 128", len(token))
	}

	w = post(t, h.VerifyEmail, access.Anonymous, "/api/v1/verification/:token", "/api/v1/verification/"+token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("the delivered token did not verify the email: %d: %s", w.Code, w.Body.String())
	}

	user, err := h.DB.GetUserByEmail(context.Background(), "jane@example.com")
	if err != nil {
		t.Fatalf("failed to read the registered learner: %v", err)
	}
	if !user.EmailVerified {
		t.Fatalf("verifying the delivered token left isVerified false")
	}
}

// Resend is the recovery path: it issues a fresh token and asks for delivery.
func TestResendVerificationIssuesAFreshTokenAndDelivers(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	delivery := newFakeMail()
	h.Mail = delivery

	w := post(t, h.ResendVerificationEmail, learner, "/api/v1/resend-verification", "/api/v1/resend-verification", "")

	if w.Code != http.StatusOK {
		t.Fatalf("ResendVerificationEmail answered %d: %s", w.Code, w.Body.String())
	}
	if len(delivery.verifications) != 1 {
		t.Fatalf("delivery was attempted %d times, want 1", len(delivery.verifications))
	}
	if got := delivery.verifications[0]; got.to != "learner@example.com" || got.token == "" {
		t.Fatalf("resent verification = %+v, want the learner's address and a token", got)
	}
}

// Forgot password actually delivers a reset message, and the token it carries
// is the one that resets the password.
func TestForgotPasswordDeliversAResetTokenThatResetsThePassword(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	delivery := newFakeMail()
	h.Mail = delivery

	w := post(t, h.ForgotPassword, access.Anonymous, "/api/v1/forgot-password", "/api/v1/forgot-password", `{"email":"learner@example.com"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ForgotPassword answered %d: %s", w.Code, w.Body.String())
	}
	if len(delivery.resets) != 1 {
		t.Fatalf("reset delivery was attempted %d times, want 1", len(delivery.resets))
	}

	sent := delivery.resets[0]
	if sent.to != "learner@example.com" {
		t.Fatalf("reset sent to %q, want learner@example.com", sent.to)
	}
	if sent.expiresIn != time.Hour {
		t.Fatalf("reset email states a lifetime of %v, want 1h", sent.expiresIn)
	}
	if !strings.HasPrefix(sent.token, "sec_rst_") {
		t.Fatalf("reset token = %q, want the sec_rst_ prefix", sent.token)
	}
	if len(sent.token) != len("sec_rst_")+48 {
		t.Fatalf("reset token carries %d characters after the prefix, want 48", len(sent.token)-len("sec_rst_"))
	}

	w = post(t, h.ResetPassword, access.Anonymous, "/api/v1/reset-password/:token", "/api/v1/reset-password/"+sent.token, `{"password":"newPassword123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("the delivered reset token did not reset the password: %d: %s", w.Code, w.Body.String())
	}

	user, err := h.DB.GetUser(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the learner: %v", err)
	}
	if !helpers.CheckPasswordHash("newPassword123", user.PasswordHash) {
		t.Fatalf("the password was not replaced by the reset")
	}
}

// Forgot password always answers 200, so the endpoint cannot be used to learn
// which addresses have accounts, and an outage does not tell the caller either.
func TestForgotPasswordAlwaysAnswersOK(t *testing.T) {
	t.Run("unknown address", func(t *testing.T) {
		h := newTestHandler(t)
		delivery := newFakeMail()
		h.Mail = delivery

		w := post(t, h.ForgotPassword, access.Anonymous, "/api/v1/forgot-password", "/api/v1/forgot-password", `{"email":"nobody@example.com"}`)

		if w.Code != http.StatusOK {
			t.Fatalf("ForgotPassword answered %d for an unknown address: %s", w.Code, w.Body.String())
		}
		if len(delivery.resets) != 0 {
			t.Fatalf("reset delivery was attempted for an unknown address")
		}
	})

	t.Run("delivery failed", func(t *testing.T) {
		h := newTestHandler(t)
		dbtest.SeedUser(t, "learner@example.com")
		delivery := &fakeMail{resetResult: mail.Result{Outcome: mail.Failed, Err: errors.New("smtp down")}}
		h.Mail = delivery

		w := post(t, h.ForgotPassword, access.Anonymous, "/api/v1/forgot-password", "/api/v1/forgot-password", `{"email":"learner@example.com"}`)

		if w.Code != http.StatusOK {
			t.Fatalf("ForgotPassword answered %d when delivery failed: %s", w.Code, w.Body.String())
		}
		if len(delivery.resets) != 1 {
			t.Fatalf("reset delivery was attempted %d times, want 1", len(delivery.resets))
		}
	})
}
