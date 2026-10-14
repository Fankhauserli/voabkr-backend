package access_test

import (
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func deck(owner int64, isPublic bool) db.Deck {
	d := db.Deck{ID: 1, Name: "TOPIK I Vocabulary", Type: db.DeckTypeVocabulary, IsPublic: isPublic}
	if owner > 0 {
		d.OwnerID = pgtype.Int8{Int64: owner, Valid: true}
	}
	return d
}

func share(deckID, userID int64, permission string) *db.DeckShare {
	return &db.DeckShare{
		ID:         1,
		DeckID:     deckID,
		UserID:     pgtype.Int8{Int64: userID, Valid: true},
		Permission: permission,
	}
}

func TestRole(t *testing.T) {
	tests := []struct {
		name     string
		deck     db.Deck
		share    *db.DeckShare
		callerID int64
		want     string
	}{
		{
			name:     "owner of their own deck",
			deck:     deck(7, false),
			callerID: 7,
			want:     access.RoleOwner,
		},
		{
			name:     "holder of an edit share",
			deck:     deck(7, false),
			share:    share(1, 9, "edit"),
			callerID: 9,
			want:     access.RoleEdit,
		},
		{
			name:     "holder of a view share",
			deck:     deck(7, false),
			share:    share(1, 9, "view"),
			callerID: 9,
			want:     access.RoleView,
		},
		{
			name:     "learner with no share on someone else's deck",
			deck:     deck(7, true),
			callerID: 9,
			want:     access.RolePublic,
		},
		{
			name:     "anonymous caller reading a public deck",
			deck:     deck(7, true),
			callerID: access.Anonymous,
			want:     access.RolePublic,
		},
		{
			name:     "anonymous caller reading a system deck",
			deck:     deck(0, true),
			callerID: access.Anonymous,
			want:     access.RolePublic,
		},
		{
			name:     "another learner's share never confers a role",
			deck:     deck(7, false),
			share:    share(1, 11, "edit"),
			callerID: 9,
			want:     access.RolePublic,
		},
		{
			name:     "a share on another deck never confers a role",
			deck:     deck(7, false),
			share:    share(2, 9, "edit"),
			callerID: 9,
			want:     access.RolePublic,
		},
		{
			name:     "an ownerless deck has no owner",
			deck:     deck(0, false),
			callerID: 9,
			want:     access.RolePublic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := access.Role(tt.deck, tt.share, tt.callerID); got != tt.want {
				t.Errorf("Role() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanWriteCards(t *testing.T) {
	tests := []struct {
		name string
		deck db.Deck
		role string
		want bool
	}{
		{name: "the owner writes their own deck's cards", deck: deck(7, true), role: access.RoleOwner, want: true},
		{name: "a holder of an edit permission writes them", deck: deck(7, false), role: access.RoleEdit, want: true},
		{name: "a holder of a view permission only reads", deck: deck(7, true), role: access.RoleView, want: false},
		{name: "an anonymous caller writes nothing", deck: deck(7, true), role: access.RolePublic, want: false},
		{name: "a system deck is never written, even by a share holder", deck: deck(0, true), role: access.RoleEdit, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := access.CanWriteCards(tt.deck, tt.role); got != tt.want {
				t.Errorf("CanWriteCards() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanRead(t *testing.T) {
	tests := []struct {
		name string
		deck db.Deck
		role string
		want bool
	}{
		{name: "public deck, public role", deck: deck(7, true), role: access.RolePublic, want: true},
		{name: "public deck, owner role", deck: deck(7, true), role: access.RoleOwner, want: true},
		{name: "private deck, owner role", deck: deck(7, false), role: access.RoleOwner, want: true},
		{name: "private deck, edit role", deck: deck(7, false), role: access.RoleEdit, want: true},
		{name: "private deck, view role", deck: deck(7, false), role: access.RoleView, want: true},
		{name: "private deck, public role", deck: deck(7, false), role: access.RolePublic, want: false},
		{name: "private system deck, public role", deck: deck(0, false), role: access.RolePublic, want: false},
		{name: "private system deck, held share", deck: deck(0, false), role: access.RoleEdit, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := access.CanRead(tt.deck, tt.role); got != tt.want {
				t.Errorf("CanRead() = %v, want %v", got, tt.want)
			}
		})
	}
}
