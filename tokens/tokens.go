// Package tokens mints the opaque tokens the product's links carry. One entropy
// source and one error policy serve every kind, so no flow can silently issue an
// empty token: a failure is returned and the caller decides what to do.
package tokens

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Kind is a flow whose token shape links and clients already depend on.
type Kind int

const (
	// ShareLink is a Deck's share token: "sec_" and 16 random bytes.
	ShareLink Kind = iota

	// Verification is an email verification token: 64 random bytes, no prefix.
	Verification

	// PasswordReset is a password reset token: "sec_rst_" and 24 random bytes.
	PasswordReset
)

// Issuer mints tokens from one entropy source.
type Issuer struct {
	entropy io.Reader
}

// NewIssuer returns an Issuer over the operating system's entropy.
func NewIssuer() *Issuer {
	return &Issuer{entropy: rand.Reader}
}

// Mint returns a fresh token for kind. The shapes are frozen: a share link is
// "sec_" plus 16 bytes, a verification token is 64 bytes, and a reset token is
// "sec_rst_" plus 24 bytes, all as lowercase hex. An error means no token was
// minted; Mint never returns an empty token with a nil error.
func (i *Issuer) Mint(kind Kind) (string, error) {
	var (
		prefix string
		size   int
	)

	switch kind {
	case ShareLink:
		prefix, size = "sec_", 16
	case Verification:
		size = 64
	case PasswordReset:
		prefix, size = "sec_rst_", 24
	default:
		return "", fmt.Errorf("tokens: cannot mint unknown kind %d", kind)
	}

	b := make([]byte, size)
	if _, err := io.ReadFull(i.entropy, b); err != nil {
		return "", fmt.Errorf("tokens: mint kind %d: %w", kind, err)
	}

	return prefix + hex.EncodeToString(b), nil
}
