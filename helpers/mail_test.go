package helpers

import (
	"fmt"
	"mime"
	"strings"
	"testing"
)

func TestRenderVerificationEmail(t *testing.T) {
	data := VerificationEmailData{
		Name:            "Jane Doe",
		Email:           "jane@example.com",
		VerificationURL: "http://localhost:5173/verify-email?token=abcdef123456",
		Token:           "abcdef123456",
		ExpireHours:     24,
		Year:            2026,
	}

	plainText, htmlContent, err := renderVerificationEmail(data)
	if err != nil {
		t.Fatalf("renderVerificationEmail failed: %v", err)
	}

	// Verify plain text contents
	if !strings.Contains(plainText, "Jane Doe") {
		t.Errorf("plain text missing user name: %s", plainText)
	}
	if !strings.Contains(plainText, data.VerificationURL) {
		t.Errorf("plain text missing verification URL: %s", plainText)
	}

	// Verify HTML contents
	if !strings.Contains(htmlContent, "Jane Doe") {
		t.Errorf("html content missing user name: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, data.VerificationURL) {
		t.Errorf("html content missing verification URL: %s", htmlContent)
	}
	if !strings.Contains(htmlContent, "voabkr") {
		t.Errorf("html content missing brand title: %s", htmlContent)
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

	// Verify headers (before the first double CRLF) are strictly 7-bit ASCII to prevent SMTPUTF8 bounce
	headerEnd := strings.Index(msgStr, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatalf("MIME message missing header separator")
	}
	headers := msgStr[:headerEnd]
	for _, r := range headers {
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
