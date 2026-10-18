package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/account"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// newAccount returns the account module over the test database and a local-only
// cache.
func newAccount(t *testing.T) (*account.Account, *db.Queries) {
	t.Helper()

	queries := dbtest.Setup(t)

	c, err := cache.NewTwoLevelCache(cache.Config{EnableLocalCache: true})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(c.Close)

	return account.New(queries, c), queries
}

// verifiedLearner returns the module and a learner whose address is verified and
// whose profile entry is warm, so a later read proves what the write changed.
func verifiedLearner(t *testing.T) (*account.Account, int64) {
	t.Helper()

	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if err := a.MarkEmailVerified(ctx, learner); err != nil {
		t.Fatalf("failed to mark the email verified: %v", err)
	}
	before, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to warm the profile: %v", err)
	}
	if !before.IsVerified {
		t.Fatal("expected the seeded learner to be verified")
	}
	return a, learner
}

// learnerWithVerificationToken returns the module, its queries, the learner and a
// pending verification token, so a write can be checked against the link it owns.
func learnerWithVerificationToken(t *testing.T) (*account.Account, *db.Queries, int64, string) {
	t.Helper()

	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	token := "verification-token"

	if _, err := queries.CreateVerificationToken(context.Background(), db.CreateVerificationTokenParams{
		UserID:    learner,
		Token:     token,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("failed to create a verification token: %v", err)
	}
	return a, queries, learner, token
}

// The profile read carries the learner's row as the wire names it.
func TestProfileReadsTheLearner(t *testing.T) {
	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	profile, err := a.Profile(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the profile: %v", err)
	}

	if int64(profile.Id) != learner || profile.Name != "learner@example.com" || profile.Email != "learner@example.com" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if !profile.IsActive || profile.IsVerified {
		t.Fatalf("expected an active, unverified learner, got active=%v verified=%v", profile.IsActive, profile.IsVerified)
	}
}

// A profile read is answered from the entry the first read wrote: a change that
// went around the module waits for the entry to be cleared.
func TestProfileServesAWarmEntry(t *testing.T) {
	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if _, err := a.Profile(ctx, learner); err != nil {
		t.Fatalf("failed to warm the profile: %v", err)
	}

	renameLearner(t, queries, learner, "Renamed")

	profile, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the warm profile: %v", err)
	}
	if profile.Name != "learner@example.com" {
		t.Fatalf("expected the warm entry's name, got %q", profile.Name)
	}
}

// Updating the profile clears the entry, so the next read is the new row.
func TestUpdateProfileIsVisibleOnTheNextRead(t *testing.T) {
	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if _, err := a.Profile(ctx, learner); err != nil {
		t.Fatalf("failed to warm the profile: %v", err)
	}

	if err := a.UpdateProfile(ctx, learner, "Renamed", "renamed@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	profile, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the profile: %v", err)
	}
	if profile.Name != "Renamed" || profile.Email != "renamed@example.com" {
		t.Fatalf("expected the updated profile, got %+v", profile)
	}
}

// Changing the address clears the verification that answered for the old one:
// the next profile read reports the learner unverified.
func TestUpdateProfileChangingTheEmailClearsVerification(t *testing.T) {
	a, learner := verifiedLearner(t)
	ctx := context.Background()

	if err := a.UpdateProfile(ctx, learner, "Learner", "new@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	after, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the profile: %v", err)
	}
	if after.IsVerified {
		t.Fatal("expected a changed address to clear the verification")
	}
}

// A name-only edit leaves verification standing: an unrelated change does not
// make the learner prove their address again.
func TestUpdateProfileKeepingTheEmailKeepsVerification(t *testing.T) {
	a, learner := verifiedLearner(t)
	ctx := context.Background()

	if err := a.UpdateProfile(ctx, learner, "Renamed", "learner@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	after, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the profile: %v", err)
	}
	if !after.IsVerified {
		t.Fatal("expected an unchanged address to keep the verification")
	}
}

// Changing the address retires the links mailed to the old one, so a stale link
// cannot verify an address it was never issued for.
func TestUpdateProfileChangingTheEmailClearsVerificationTokens(t *testing.T) {
	a, queries, learner, token := learnerWithVerificationToken(t)
	ctx := context.Background()

	if err := a.UpdateProfile(ctx, learner, "Learner", "new@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	if _, err := queries.GetVerificationToken(ctx, token); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected the old address's verification token to be gone, got %v", err)
	}
}

// An unchanged address keeps its pending links: a name-only edit does not make
// the learner wait for a new message.
func TestUpdateProfileKeepingTheEmailKeepsVerificationTokens(t *testing.T) {
	a, queries, learner, token := learnerWithVerificationToken(t)
	ctx := context.Background()

	if err := a.UpdateProfile(ctx, learner, "Renamed", "learner@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	if _, err := queries.GetVerificationToken(ctx, token); err != nil {
		t.Fatalf("expected the unchanged address's verification token to stand, got %v", err)
	}
}

// A profile update leaves the stored password alone: an unrelated edit does not
// change the learner's ability to sign in.
func TestUpdateProfileKeepsThePassword(t *testing.T) {
	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	if err := a.UpdateProfile(context.Background(), learner, "Renamed", "learner@example.com"); err != nil {
		t.Fatalf("failed to update the profile: %v", err)
	}

	user, err := queries.GetUser(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the learner: %v", err)
	}
	if user.PasswordHash != "not-a-real-hash" {
		t.Fatalf("expected the password hash to stand, got %q", user.PasswordHash)
	}
}

// Changing to an address another learner holds is refused as a conflict, and the
// row is left as it was.
func TestUpdateProfileRefusesADuplicateEmail(t *testing.T) {
	a, queries := newAccount(t)
	dbtest.SeedUser(t, "taken@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")

	err := a.UpdateProfile(context.Background(), learner, "Learner", "taken@example.com")
	if !errors.Is(err, account.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}

	user, err := queries.GetUser(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the learner: %v", err)
	}
	if user.Email != "learner@example.com" {
		t.Fatalf("expected the learner's address to stand, got %q", user.Email)
	}
}

// A learner with no settings row is given one carrying the defaults, including
// the Study direction.
func TestSettingsCreateTheMissingRowWithDefaults(t *testing.T) {
	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	settings, err := a.Settings(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the settings: %v", err)
	}

	if settings.CardsPerDay != 20 || settings.StudyDirection != "koreanToEnglish" || settings.ScratchPadEnabled {
		t.Fatalf("unexpected default settings: %+v", settings)
	}
	if _, err := queries.GetUserSettings(context.Background(), learner); err != nil {
		t.Fatalf("expected the settings row to exist: %v", err)
	}
}

// A settings read is answered from the entry the first read wrote.
func TestSettingsServeAWarmEntry(t *testing.T) {
	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if _, err := a.Settings(ctx, learner); err != nil {
		t.Fatalf("failed to warm the settings: %v", err)
	}

	if err := queries.UpdateUserSettings(ctx, db.UpdateUserSettingsParams{
		UserID:            learner,
		CardsPerDay:       40,
		StudyDirection:    "englishToKorean",
		ScratchPadEnabled: true,
	}); err != nil {
		t.Fatalf("failed to change the settings behind the module: %v", err)
	}

	settings, err := a.Settings(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the warm settings: %v", err)
	}
	if settings.CardsPerDay != 20 || settings.StudyDirection != "koreanToEnglish" || settings.ScratchPadEnabled {
		t.Fatalf("expected the warm entry's settings, got %+v", settings)
	}
}

// A settings update is reflected on the next read.
func TestUpdateSettingsIsVisibleOnTheNextRead(t *testing.T) {
	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if _, err := a.Settings(ctx, learner); err != nil {
		t.Fatalf("failed to warm the settings: %v", err)
	}

	cardsPerDay := uint32(40)
	direction := "englishToKorean"
	scratchPad := true
	updated, err := a.UpdateSettings(ctx, learner, &types.UpdateSettingsRequest{
		CardsPerDay:       &cardsPerDay,
		StudyDirection:    &direction,
		ScratchPadEnabled: &scratchPad,
	})
	if err != nil {
		t.Fatalf("failed to update the settings: %v", err)
	}
	if updated.CardsPerDay != 40 || updated.StudyDirection != "englishToKorean" || !updated.ScratchPadEnabled {
		t.Fatalf("unexpected updated settings: %+v", updated)
	}

	settings, err := a.Settings(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the settings: %v", err)
	}
	if settings.CardsPerDay != 40 || settings.StudyDirection != "englishToKorean" || !settings.ScratchPadEnabled {
		t.Fatalf("expected the updated settings on the next read, got %+v", settings)
	}
}

// A partial update keeps the default Study direction and the other settings.
func TestUpdateSettingsKeepsTheDefaultStudyDirection(t *testing.T) {
	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	cardsPerDay := uint32(40)
	updated, err := a.UpdateSettings(context.Background(), learner, &types.UpdateSettingsRequest{
		CardsPerDay: &cardsPerDay,
	})
	if err != nil {
		t.Fatalf("failed to update the settings: %v", err)
	}

	if updated.CardsPerDay != 40 || updated.StudyDirection != "koreanToEnglish" || updated.ScratchPadEnabled {
		t.Fatalf("expected the default direction and the new card count, got %+v", updated)
	}
}

// Verifying an email clears the cached profile, so the next read reports what
// the row now says.
func TestMarkEmailVerifiedInvalidatesTheProfile(t *testing.T) {
	a, _ := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	before, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to warm the profile: %v", err)
	}
	if before.IsVerified {
		t.Fatal("expected the seeded learner to be unverified")
	}

	if err := a.MarkEmailVerified(ctx, learner); err != nil {
		t.Fatalf("failed to mark the email verified: %v", err)
	}

	after, err := a.Profile(ctx, learner)
	if err != nil {
		t.Fatalf("failed to read the profile: %v", err)
	}
	if !after.IsVerified {
		t.Fatal("expected the next profile read to report verified")
	}
}

// Erasure removes the learner and the entries holding their body, so the next
// read does not answer from a profile or settings row describing someone gone.
func TestEraseRemovesTheProfileEntryAndTheLearner(t *testing.T) {
	a, queries := newAccount(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	ctx := context.Background()

	if _, err := a.Profile(ctx, learner); err != nil {
		t.Fatalf("failed to warm the profile: %v", err)
	}
	if _, err := a.Settings(ctx, learner); err != nil {
		t.Fatalf("failed to warm the settings: %v", err)
	}

	if err := a.Erase(ctx, learner); err != nil {
		t.Fatalf("failed to erase the learner: %v", err)
	}

	if _, err := a.Profile(ctx, learner); err == nil {
		t.Fatal("expected the erased learner's profile read to fail")
	}
	if _, err := a.Settings(ctx, learner); err == nil {
		t.Fatal("expected the erased learner's settings read to fail")
	}

	if _, err := queries.GetUser(ctx, learner); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected the learner to be gone, got %v", err)
	}
}

// renameLearner changes the learner's name through the database, leaving the
// cache alone — a write the module did not announce.
func renameLearner(t *testing.T, queries *db.Queries, learner int64, name string) {
	t.Helper()

	user, err := queries.GetUser(context.Background(), learner)
	if err != nil {
		t.Fatalf("failed to read the learner: %v", err)
	}
	if _, err := queries.UpdateUser(context.Background(), db.UpdateUserParams{
		ID:    user.ID,
		Name:  name,
		Email: user.Email,
	}); err != nil {
		t.Fatalf("failed to rename the learner: %v", err)
	}
}
