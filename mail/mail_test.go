package mail

import (
	"bytes"
	"fmt"
	"log"
	"mime"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

const (
	testVerificationToken = "a1b2c3d4e5f6"
	testResetToken        = "sec_rst_a1b2c3d4e5f6"
)

func configured() Config {
	return Config{
		Addr:        "smtp.example.com:587",
		User:        "mailer@example.com",
		Pass:        "secret",
		From:        "no-reply@example.com",
		FrontendURL: "https://app.example.com",
	}
}

// captureLog replaces the standard logger's destination for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

type sentMessage struct {
	addr string
	auth smtp.Auth
	from string
	to   []string
	msg  []byte
}

type fakeTransport struct {
	sent []sentMessage
	err  error
}

func (f *fakeTransport) Send(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	f.sent = append(f.sent, sentMessage{addr: addr, auth: auth, from: from, to: to, msg: msg})
	return f.err
}

func TestSendVerificationDeliversThroughTheTransport(t *testing.T) {
	transport := &fakeTransport{}
	service := newWithTransport(configured(), transport)

	result := service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)

	if result.Outcome != Delivered {
		t.Fatalf("outcome = %v, want Delivered (err: %v)", result.Outcome, result.Err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("transport sent %d messages, want 1", len(transport.sent))
	}

	sent := transport.sent[0]
	if sent.addr != "smtp.example.com:587" {
		t.Errorf("sent to addr %q, want smtp.example.com:587", sent.addr)
	}
	if sent.from != "no-reply@example.com" {
		t.Errorf("sent from %q, want no-reply@example.com", sent.from)
	}
	if len(sent.to) != 1 || sent.to[0] != "learner@example.com" {
		t.Errorf("sent to %v, want [learner@example.com]", sent.to)
	}
	if sent.auth == nil {
		t.Errorf("configured credentials produced no SMTP auth")
	}

	body := string(sent.msg)
	for _, want := range []string{
		"To: learner@example.com",
		"Subject: " + mime.QEncoding.Encode("UTF-8", "Verify your voabkr account | 이메일 인증"),
		"https://app.example.com/verify-email?token=" + testVerificationToken,
		"Jane",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("verification message missing %q", want)
		}
	}
}

func TestSendPasswordResetDeliversThroughTheTransport(t *testing.T) {
	transport := &fakeTransport{}
	service := newWithTransport(configured(), transport)

	result := service.SendPasswordReset("learner@example.com", "Jane", testResetToken, time.Hour)

	if result.Outcome != Delivered {
		t.Fatalf("outcome = %v, want Delivered (err: %v)", result.Outcome, result.Err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("transport sent %d messages, want 1", len(transport.sent))
	}

	body := string(transport.sent[0].msg)
	for _, want := range []string{
		"To: learner@example.com",
		"Subject: " + mime.QEncoding.Encode("UTF-8", "Reset your voabkr password | 비밀번호 재설정"),
		"https://app.example.com/reset-password?token=" + testResetToken,
		"Jane",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reset message missing %q", want)
		}
	}
}

// An instance with no SMTP configuration is skipped, not failed: the outcome
// distinguishes a deliberate skip from an outage.
func TestSendSkipsWhenUnconfigured(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  Config
	}{
		{"no address", Config{From: "no-reply@example.com"}},
		{"no sender", Config{Addr: "smtp.example.com:587"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport := &fakeTransport{}
			service := newWithTransport(tt.cfg, transport)

			verification := service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)
			reset := service.SendPasswordReset("learner@example.com", "Jane", testResetToken, time.Hour)

			if verification.Outcome != Skipped || reset.Outcome != Skipped {
				t.Fatalf("outcomes = %v and %v, want Skipped", verification.Outcome, reset.Outcome)
			}
			if len(transport.sent) != 0 {
				t.Fatalf("transport sent %d messages while unconfigured, want 0", len(transport.sent))
			}
		})
	}
}

func TestSendFailsWhenTheTransportFails(t *testing.T) {
	transport := &fakeTransport{err: fmt.Errorf("connection refused")}
	service := newWithTransport(configured(), transport)

	result := service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)

	if result.Outcome != Failed {
		t.Fatalf("outcome = %v, want Failed", result.Outcome)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "connection refused") {
		t.Fatalf("failure does not carry the transport error: %v", result.Err)
	}
}

// A skipped verification's token is printed for a developer only in debug mode.
// Production logs never carry a secret token.
func TestSkippedVerificationLogsTokenOnlyInDebugMode(t *testing.T) {
	for _, tt := range []struct {
		name  string
		debug bool
		want  bool
	}{
		{"debug", true, true},
		{"release", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configured()
			cfg.Addr = ""
			cfg.Debug = tt.debug

			logs := captureLog(t)
			service := newWithTransport(cfg, &fakeTransport{})
			service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)

			if got := strings.Contains(logs.String(), testVerificationToken); got != tt.want {
				t.Fatalf("token in log = %v, want %v; log: %s", got, tt.want, logs.String())
			}
		})
	}
}

func TestDeliveredVerificationNeverLogsItsToken(t *testing.T) {
	logs := captureLog(t)
	service := newWithTransport(configured(), &fakeTransport{})

	service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)
	service.SendPasswordReset("learner@example.com", "Jane", testResetToken, time.Hour)

	for _, token := range []string{testVerificationToken, testResetToken} {
		if strings.Contains(logs.String(), token) {
			t.Fatalf("delivered mail logged token %q: %s", token, logs.String())
		}
	}
}

// Printing a skipped token is an explicit opt-in. An unset GIN_MODE defaults to
// Gin's debug mode, so treating it as debug would put secrets in the logs of a
// production deployment that forgot to set release.
func TestFromEnvPrintsTokensOnlyForAnExplicitDebugMode(t *testing.T) {
	for _, tt := range []struct {
		mode string
		want bool
	}{
		{"", false},
		{"debug", true},
		{"release", false},
		{"test", false},
	} {
		t.Run("GIN_MODE="+tt.mode, func(t *testing.T) {
			t.Setenv("GIN_MODE", tt.mode)

			if got := FromEnv().Debug; got != tt.want {
				t.Fatalf("FromEnv().Debug = %v for GIN_MODE=%q, want %v", got, tt.mode, tt.want)
			}
		})
	}
}

// Configuration is read when the service is constructed; later environment
// changes cannot redirect a message.
func TestConfigurationIsReadOnce(t *testing.T) {
	t.Setenv("SMTP_ADDR", "first.example.com:587")
	t.Setenv("FROM_EMAIL", "no-reply@example.com")

	transport := &fakeTransport{}
	service := newWithTransport(FromEnv(), transport)

	t.Setenv("SMTP_ADDR", "second.example.com:587")
	service.SendVerification("learner@example.com", "Jane", testVerificationToken, 24*time.Hour)

	if len(transport.sent) != 1 {
		t.Fatalf("transport sent %d messages, want 1", len(transport.sent))
	}
	if transport.sent[0].addr != "first.example.com:587" {
		t.Fatalf("message went to %q, want the address configuration named at construction", transport.sent[0].addr)
	}
}

// Rendering is pure: no transport and no environment are involved.
func TestRenderVerificationEmail(t *testing.T) {
	data := emailData{
		Name:        "Jane Doe",
		Email:       "jane@example.com",
		LinkURL:     "http://localhost:5173/verify-email?token=abcdef123456",
		Token:       "abcdef123456",
		ExpireHours: 24,
		Year:        2026,
	}

	plainText, htmlContent, err := renderEmail("verification", data)
	if err != nil {
		t.Fatalf("renderEmail failed: %v", err)
	}

	for _, content := range []string{plainText, htmlContent} {
		if !strings.Contains(content, "Jane Doe") {
			t.Errorf("content missing user name: %s", content)
		}
		if !strings.Contains(content, data.LinkURL) {
			t.Errorf("content missing verification URL: %s", content)
		}
	}
	if !strings.Contains(htmlContent, "voabkr") {
		t.Errorf("html content missing brand title: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, "#C85232") {
		t.Errorf("html content missing terracotta brand color: %s", htmlContent)
	}
}

func TestRenderPasswordResetEmail(t *testing.T) {
	data := emailData{
		Name:        "Jane Doe",
		Email:       "jane@example.com",
		LinkURL:     "http://localhost:5173/reset-password?token=sec_rst_123456",
		Token:       "sec_rst_123456",
		ExpireHours: 1,
		Year:        2026,
	}

	plainText, htmlContent, err := renderEmail("reset", data)
	if err != nil {
		t.Fatalf("renderEmail failed: %v", err)
	}

	for _, content := range []string{plainText, htmlContent} {
		if !strings.Contains(content, "Jane Doe") {
			t.Errorf("content missing user name: %s", content)
		}
		if !strings.Contains(content, data.LinkURL) {
			t.Errorf("content missing reset URL: %s", content)
		}
	}
	if !strings.Contains(htmlContent, "#C85232") {
		t.Errorf("html content missing terracotta brand color: %s", htmlContent)
	}
}

func TestBuildMIMEMessage(t *testing.T) {
	from := "no-reply@voabkr.com"
	to := "user@example.com"
	subject := "Verify your voabkr account | 이메일 인증"
	textBody := "Plain text body with link: http://localhost:5173/verify-email?token=123"
	htmlBody := "<p>HTML body with link</p>"

	msg := buildMIMEMessage(from, to, subject, textBody, htmlBody)
	msgStr := string(msg)

	if !strings.Contains(msgStr, "From: no-reply@voabkr.com") {
		t.Errorf("MIME message missing From header")
	}
	if !strings.Contains(msgStr, "To: user@example.com") {
		t.Errorf("MIME message missing To header")
	}
	expectedSubject := fmt.Sprintf("Subject: %s", mime.QEncoding.Encode("UTF-8", subject))
	if !strings.Contains(msgStr, expectedSubject) {
		t.Errorf("MIME message missing or incorrectly encoded Subject header. Expected: %s", expectedSubject)
	}

	// Headers (before the first double CRLF) must be strictly 7-bit ASCII to
	// prevent an SMTPUTF8 bounce.
	headerEnd := strings.Index(msgStr, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatalf("MIME message missing header separator")
	}
	for _, r := range msgStr[:headerEnd] {
		if r > 127 {
			t.Errorf("MIME headers contain non-ASCII character %q which triggers SMTPUTF8 bounce", r)
		}
	}

	if !strings.Contains(msgStr, "multipart/alternative") {
		t.Errorf("MIME message missing multipart/alternative content-type")
	}
	if !strings.Contains(msgStr, textBody) {
		t.Errorf("MIME message missing text body")
	}
	if !strings.Contains(msgStr, htmlBody) {
		t.Errorf("MIME message missing html body")
	}
}
