// Package mail delivers the product's account email. It is delivery-only: it
// renders, assembles and hands a message to SMTP, and never reads or writes the
// database. Configuration is read once when the service is constructed, and
// every attempt reports one outcome — delivered, skipped because this instance
// has no SMTP configuration, or failed with the transport error.
package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/Fankhauserli/voabkr-backend/templates"
)

// Outcome is how an attempt to deliver one message ended.
type Outcome int

const (
	// Unknown is the zero value: no attempt was reported. It never claims
	// delivery, so a Result built without an outcome cannot read as success.
	Unknown Outcome = iota

	// Delivered means the transport accepted the message.
	Delivered

	// Skipped means this instance has no SMTP configuration, so nothing was
	// sent. It is a deliberate state, not a failure.
	Skipped

	// Failed means delivery was attempted and the transport returned an error.
	Failed
)

func (o Outcome) String() string {
	switch o {
	case Unknown:
		return "unknown"
	case Delivered:
		return "delivered"
	case Skipped:
		return "skipped"
	case Failed:
		return "failed"
	default:
		return fmt.Sprintf("outcome(%d)", int(o))
	}
}

// Result is the one outcome of an attempt to deliver one message.
type Result struct {
	Outcome Outcome
	Err     error
}

// Mailer delivers the account messages a handler asks for. A handler holds this
// interface, so delivery behaviour can be tested with a fake outcome.
//
// expiresIn is how long the token the message carries stays valid. The caller
// that stored the token owns its lifetime and passes the same value here, so the
// message cannot state a validity the stored token does not have.
type Mailer interface {
	SendVerification(to, name, token string, expiresIn time.Duration) Result
	SendPasswordReset(to, name, token string, expiresIn time.Duration) Result
}

// Config is the SMTP and link configuration read once when a Service is built.
type Config struct {
	Addr        string
	User        string
	Pass        string
	From        string
	FrontendURL string

	// Debug allows a skipped verification's token to be printed so a developer
	// without SMTP can complete the flow. Production logs never carry a token.
	Debug bool
}

// FromEnv reads delivery configuration from the environment exactly once.
func FromEnv() Config {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = os.Getenv("APP_URL")
	}

	mode := os.Getenv("GIN_MODE")
	return Config{
		Addr:        os.Getenv("SMTP_ADDR"),
		User:        os.Getenv("SMTP_USER"),
		Pass:        os.Getenv("SMTP_PASS"),
		From:        os.Getenv("FROM_EMAIL"),
		FrontendURL: frontendURL,
		// Printing a token is an explicit opt-in: an unset GIN_MODE defaults to
		// Gin's debug mode, and a production deployment that forgets to set
		// release must not start writing secrets to its logs.
		Debug: mode == "debug",
	}
}

// transport is the internal seam between the module and SMTP, so tests can
// observe delivery without a server.
type transport interface {
	Send(addr string, auth smtp.Auth, from string, to []string, msg []byte) error
}

type smtpTransport struct{}

func (smtpTransport) Send(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, auth, from, to, msg)
}

// Service renders and delivers account mail.
type Service struct {
	cfg       Config
	auth      smtp.Auth
	transport transport
}

// New builds a Service over the given configuration, which it reads once.
func New(cfg Config) *Service {
	return newWithTransport(cfg, smtpTransport{})
}

// NewFromEnv builds a Service over the environment.
func NewFromEnv() *Service {
	return New(FromEnv())
}

func newWithTransport(cfg Config, t transport) *Service {
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	if cfg.FrontendURL == "" {
		cfg.FrontendURL = "http://localhost:5173" // Vite default dev port
	}

	var auth smtp.Auth
	if cfg.User != "" && cfg.Pass != "" {
		host, _, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			host = cfg.Addr
		}
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, host)
	}

	return &Service{cfg: cfg, auth: auth, transport: t}
}

// configured reports whether this instance has enough SMTP configuration to
// attempt delivery.
func (s *Service) configured() bool {
	return s.cfg.Addr != "" && s.cfg.From != ""
}

// SendVerification delivers the email that lets a learner verify their address.
func (s *Service) SendVerification(to, name, token string, expiresIn time.Duration) Result {
	message := outgoing{
		kind:       verificationKind,
		to:         to,
		subject:    verificationSubject,
		debugToken: token,
	}
	data := emailData{
		Name:        strings.TrimSpace(name),
		Email:       to,
		LinkURL:     s.cfg.FrontendURL + "/verify-email?token=" + token,
		Token:       token,
		ExpireHours: hoursOf(expiresIn),
		Year:        time.Now().Year(),
	}
	return s.deliver(message, func() (string, string, error) {
		return renderEmail("verification", data)
	})
}

// SendPasswordReset delivers the email that lets a learner choose a new password.
func (s *Service) SendPasswordReset(to, name, token string, expiresIn time.Duration) Result {
	message := outgoing{
		kind:    resetKind,
		to:      to,
		subject: resetSubject,
	}
	data := emailData{
		Name:        strings.TrimSpace(name),
		Email:       to,
		LinkURL:     s.cfg.FrontendURL + "/reset-password?token=" + token,
		Token:       token,
		ExpireHours: hoursOf(expiresIn),
		Year:        time.Now().Year(),
	}
	return s.deliver(message, func() (string, string, error) {
		return renderEmail("reset", data)
	})
}

// kind names a message in logs without letting a caller pass an arbitrary one.
type kind string

const (
	verificationKind kind = "verification"
	resetKind        kind = "password reset"
)

// outgoing is one message to attempt: who it is for, what to call it, and the
// secret a skipped delivery may print in debug mode.
type outgoing struct {
	kind       kind
	to         string
	subject    string
	debugToken string
}

// deliver renders and sends one message, unless this instance is unconfigured.
// The token is printed only for a skipped verification in debug mode; it never
// reaches a production log.
func (s *Service) deliver(m outgoing, render func() (string, string, error)) Result {
	if !s.configured() {
		if m.debugToken != "" && s.cfg.Debug {
			log.Printf("[INFO] mail: SMTP not configured; %s for %s skipped (token: %s)", m.kind, m.to, m.debugToken)
		} else {
			log.Printf("[INFO] mail: SMTP not configured; %s for %s skipped", m.kind, m.to)
		}
		return Result{Outcome: Skipped}
	}

	textBody, htmlBody, err := render()
	if err != nil {
		log.Printf("[ERROR] mail: failed to render %s for %s: %v", m.kind, m.to, err)
		return Result{Outcome: Failed, Err: err}
	}

	msg := buildMIMEMessage(s.cfg.From, m.to, m.subject, textBody, htmlBody)
	if err := s.transport.Send(s.cfg.Addr, s.auth, s.cfg.From, []string{m.to}, msg); err != nil {
		log.Printf("[ERROR] mail: failed to send %s to %s via %s: %v", m.kind, m.to, s.cfg.Addr, err)
		return Result{Outcome: Failed, Err: fmt.Errorf("send %s: %w", m.kind, err)}
	}

	log.Printf("[INFO] mail: %s sent to %s", m.kind, m.to)
	return Result{Outcome: Delivered}
}

// emailData carries the variables both templates use.
type emailData struct {
	Name        string
	Email       string
	LinkURL     string
	Token       string
	ExpireHours int
	Year        int
}

// hoursOf is the number of whole hours a lifetime states, never less than one.
func hoursOf(d time.Duration) int {
	hours := int(d / time.Hour)
	if hours < 1 {
		return 1
	}
	return hours
}

const (
	verificationSubject = "Verify your voabkr account | 이메일 인증"
	resetSubject        = "Reset your voabkr password | 비밀번호 재설정"
)

// renderEmail renders the HTML and plain-text versions of an email template.
// Rendering is pure: it reads the embedded templates and nothing else.
func renderEmail(name string, data any) (plainText string, htmlContent string, err error) {
	tmplHTML, err := template.ParseFS(templates.MailFS, "mail/"+name+".html")
	if err != nil {
		return "", "", fmt.Errorf("failed to parse html email template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := tmplHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute html email template: %w", err)
	}

	tmplTxt, err := template.ParseFS(templates.MailFS, "mail/"+name+".txt")
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text email template: %w", err)
	}
	var txtBuf bytes.Buffer
	if err := tmplTxt.Execute(&txtBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute text email template: %w", err)
	}

	return txtBuf.String(), htmlBuf.String(), nil
}

// buildMIMEMessage formats a multipart/alternative email body with both plaintext and HTML parts.
func buildMIMEMessage(from, to, subject, textBody, htmlBody string) []byte {
	boundary := fmt.Sprintf("voabkr_boundary_%d", time.Now().UnixNano())
	var msg bytes.Buffer

	// Headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject)))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Plain text alternative
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n\r\n")

	// HTML alternative
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	// Final boundary
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return msg.Bytes()
}
