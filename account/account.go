// Package account owns a learner's Account end to end: the profile and settings
// reads, the writes that change them, the cache entries that answer the reads,
// the row-to-wire mapping, and the invalidation every write performs.
//
// Callers ask for a profile or settings rather than assembling one, and hold no
// key. A write names the learner it settles; the module knows which entries that
// learner's body may be held under and clears them.
package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// entryTTL is how long an entry holding a profile or settings lives. A write
// clears or replaces what it holds, so this bounds only what nothing writes
// through.
const entryTTL = 10 * time.Minute

// defaultStudyDirection is the direction a learner studies in until they choose
// another. It is stored by the database's default too; this is the value a row
// that predates the column arrives with.
const defaultStudyDirection = "koreanToEnglish"

// ErrEmailTaken is returned when a profile update would give one learner the
// address another already holds.
var ErrEmailTaken = errors.New("email already taken")

// Account answers a learner's profile and settings, and applies the writes that
// change them.
type Account struct {
	db    *db.Queries
	cache cache.Cache
}

// New returns the module over the database an Account is read from and the cache
// its body is held in. A nil cache means an instance that holds nothing.
func New(q *db.Queries, c cache.Cache) *Account {
	return &Account{db: q, cache: c}
}

// The Account keys are built here and nowhere else.
func profileKey(learner int64) string { return fmt.Sprintf("user:profile:%d", learner) }

func settingsKey(learner int64) string { return fmt.Sprintf("settings:user:%d", learner) }

// Profile returns the learner's profile as the wire carries it, from the entry a
// previous read wrote or from the row on a miss.
func (a *Account) Profile(ctx context.Context, learner int64) (*types.UserResponse, error) {
	if a.cache != nil {
		var cached types.UserResponse
		if found, err := a.cache.GetProto(ctx, profileKey(learner), &cached); err == nil && found {
			return &cached, nil
		}
	}

	user, err := a.db.GetUser(ctx, learner)
	if err != nil {
		return nil, err
	}

	profile := profileOf(user)
	if a.cache != nil {
		_ = a.cache.SetProto(ctx, profileKey(learner), profile, entryTTL)
	}
	return profile, nil
}

// UpdateProfile changes the learner's name and address, and clears the entry
// holding the profile it changed. An address another learner already holds is
// ErrEmailTaken.
func (a *Account) UpdateProfile(ctx context.Context, learner int64, name, email string) error {
	err := a.db.UpdateUser(ctx, db.UpdateUserParams{
		ID:    learner,
		Name:  name,
		Email: email,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrEmailTaken
		}
		return err
	}

	a.invalidateProfile(ctx, learner)
	return nil
}

// Settings returns the learner's settings as the wire carries them, from the
// entry a previous read wrote or from the row on a miss. A learner with no row
// is given one carrying the defaults.
func (a *Account) Settings(ctx context.Context, learner int64) (*types.SettingsResponse, error) {
	if a.cache != nil {
		var cached types.SettingsResponse
		if found, err := a.cache.GetProto(ctx, settingsKey(learner), &cached); err == nil && found {
			return &cached, nil
		}
	}

	settings, err := a.storedSettings(ctx, learner)
	if err != nil {
		return nil, err
	}

	response := settingsOf(settings)
	if a.cache != nil {
		_ = a.cache.SetProto(ctx, settingsKey(learner), response, entryTTL)
	}
	return response, nil
}

// UpdateSettings applies the fields the request names, keeps the rest, and
// replaces the entry the next read answers from.
func (a *Account) UpdateSettings(ctx context.Context, learner int64, req *types.UpdateSettingsRequest) (*types.SettingsResponse, error) {
	settings, err := a.storedSettings(ctx, learner)
	if err != nil {
		return nil, err
	}

	cardsPerDay := settings.CardsPerDay
	if req.CardsPerDay != nil && *req.CardsPerDay > 0 {
		cardsPerDay = int32(*req.CardsPerDay)
	}

	studyDirection := studyDirectionOf(settings)
	if req.StudyDirection != nil && *req.StudyDirection != "" {
		studyDirection = *req.StudyDirection
	}

	scratchPadEnabled := settings.ScratchPadEnabled
	if req.ScratchPadEnabled != nil {
		scratchPadEnabled = *req.ScratchPadEnabled
	}

	err = a.db.UpdateUserSettings(ctx, db.UpdateUserSettingsParams{
		UserID:            learner,
		CardsPerDay:       cardsPerDay,
		StudyDirection:    studyDirection,
		ScratchPadEnabled: scratchPadEnabled,
	})
	if err != nil {
		return nil, err
	}

	response := &types.SettingsResponse{
		CardsPerDay:       uint32(cardsPerDay),
		StudyDirection:    studyDirection,
		ScratchPadEnabled: scratchPadEnabled,
	}
	if a.cache != nil {
		_ = a.cache.SetProto(ctx, settingsKey(learner), response, entryTTL)
	}
	return response, nil
}

// MarkEmailVerified records that the learner's address is theirs and clears the
// entry holding the profile that said otherwise, so the next read is the row.
func (a *Account) MarkEmailVerified(ctx context.Context, learner int64) error {
	if err := a.db.UpdateUserEmailVerified(ctx, learner); err != nil {
		return err
	}

	a.invalidateProfile(ctx, learner)
	return nil
}

// Erase deletes the learner and clears the entries holding their profile and
// settings, so neither answers for an Account that is gone. It does not touch
// what they owned beyond the database's own cascades; the handler erasing an
// Account also clears the Decks it leaves behind.
func (a *Account) Erase(ctx context.Context, learner int64) error {
	if err := a.db.HardDeleteUser(ctx, learner); err != nil {
		return err
	}

	if a.cache != nil {
		_ = a.cache.Delete(ctx, profileKey(learner), settingsKey(learner))
	}
	return nil
}

// storedSettings reads the learner's settings row, creating it when none is
// there. A database that cannot answer is a failure, not a missing row.
func (a *Account) storedSettings(ctx context.Context, learner int64) (db.Setting, error) {
	settings, err := a.db.GetUserSettings(ctx, learner)
	if errors.Is(err, pgx.ErrNoRows) {
		return a.db.CreateUserSettings(ctx, learner)
	}
	return settings, err
}

func (a *Account) invalidateProfile(ctx context.Context, learner int64) {
	if a.cache == nil {
		return
	}
	_ = a.cache.Delete(ctx, profileKey(learner))
}

// profileOf is the one row→wire mapping for a profile.
func profileOf(user db.User) *types.UserResponse {
	return &types.UserResponse{
		Id:         uint32(user.ID),
		Name:       user.Name,
		Email:      user.Email,
		IsVerified: user.EmailVerified,
		IsActive:   user.IsActive,
	}
}

// settingsOf is the one row→wire mapping for settings, carrying the default
// Study direction a row without one reads under.
func settingsOf(settings db.Setting) *types.SettingsResponse {
	return &types.SettingsResponse{
		CardsPerDay:       uint32(settings.CardsPerDay),
		StudyDirection:    studyDirectionOf(settings),
		ScratchPadEnabled: settings.ScratchPadEnabled,
	}
}

// studyDirectionOf is the direction a stored row studies under: its own, or the
// default when the row predates the column.
func studyDirectionOf(settings db.Setting) string {
	if settings.StudyDirection == "" {
		return defaultStudyDirection
	}
	return settings.StudyDirection
}
