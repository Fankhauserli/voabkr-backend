package tokens

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Each kind's shape is frozen: links and clients already carry these tokens.
func TestMintKeepsEachKindsFrozenShape(t *testing.T) {
	issuer := NewIssuer()

	tests := []struct {
		name   string
		kind   Kind
		prefix string
		bytes  int
	}{
		{"share link", ShareLink, "sec_", 16},
		{"verification", Verification, "", 64},
		{"password reset", PasswordReset, "sec_rst_", 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := issuer.Mint(tt.kind)
			if err != nil {
				t.Fatalf("Mint(%v) failed: %v", tt.kind, err)
			}
			if !strings.HasPrefix(token, tt.prefix) {
				t.Fatalf("Mint(%v) = %q, want prefix %q", tt.kind, token, tt.prefix)
			}

			encoded := strings.TrimPrefix(token, tt.prefix)
			if len(encoded) != tt.bytes*2 {
				t.Fatalf("Mint(%v) carries %d hex characters, want %d", tt.kind, len(encoded), tt.bytes*2)
			}
			if _, err := hex.DecodeString(encoded); err != nil {
				t.Fatalf("Mint(%v) = %q, want lowercase hex after the prefix: %v", tt.kind, token, err)
			}
		})
	}
}

func TestMintDoesNotRepeat(t *testing.T) {
	issuer := NewIssuer()

	first, err := issuer.Mint(ShareLink)
	if err != nil {
		t.Fatalf("Mint(ShareLink) failed: %v", err)
	}
	second, err := issuer.Mint(ShareLink)
	if err != nil {
		t.Fatalf("Mint(ShareLink) failed: %v", err)
	}

	if first == second {
		t.Fatalf("two minted share tokens are equal: %q", first)
	}
}

// A failed entropy read is an error, never an empty token with a nil error.
func TestMintReturnsAnErrorWhenEntropyFails(t *testing.T) {
	issuer := &Issuer{entropy: failingReader{}}

	for _, kind := range []Kind{ShareLink, Verification, PasswordReset} {
		token, err := issuer.Mint(kind)
		if err == nil {
			t.Fatalf("Mint(%v) succeeded with a failing entropy source, got %q", kind, token)
		}
		if token != "" {
			t.Fatalf("Mint(%v) returned %q alongside an error, want no token", kind, token)
		}
	}
}

func TestMintRefusesAnUnknownKind(t *testing.T) {
	issuer := NewIssuer()

	token, err := issuer.Mint(Kind(99))
	if err == nil {
		t.Fatalf("Mint(unknown) succeeded, got %q", token)
	}
	if token != "" {
		t.Fatalf("Mint(unknown) returned %q alongside an error, want no token", token)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}
