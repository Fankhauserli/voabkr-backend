-- name: GetUser :one
SELECT * FROM users
WHERE id = $1 LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1 LIMIT 1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY name;

-- name: CreateUser :one
INSERT INTO users (
  name, email, password_hash
) VALUES (
  $1, $2, $3
)
RETURNING *;

-- name: UpdateUser :exec
-- A profile edit changes the name and address alone: the password hash is not
-- part of the profile, and an unrelated edit must not change it.
UPDATE users
  set name = $2,
  email = $3,
  updated_at = NOW()
WHERE id = $1;

-- name: UpdateUserEmailVerified :exec
UPDATE users
SET email_verified = TRUE,
    updated_at = NOW()
WHERE id = $1;

-- name: DeleteUser :exec
UPDATE users
SET deleted_at = NOW()
WHERE id = $1;

-- name: HardDeleteUser :exec
DELETE FROM users
WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: GetPasswordResetToken :one
SELECT * FROM password_resets
WHERE token = $1 AND expires_at > NOW()
LIMIT 1;

-- name: CreatePasswordResetToken :one
INSERT INTO password_resets (
  user_id, token, expires_at
) VALUES (
  $1, $2, $3
)
RETURNING *;

-- name: DeletePasswordResetToken :exec
DELETE FROM password_resets
WHERE token = $1;

-- name: DeletePasswordResetTokensByUserID :exec
DELETE FROM password_resets
WHERE user_id = $1;

-- name: GetVerificationToken :one
SELECT * FROM mail_verifications
WHERE token = $1
LIMIT 1;

-- name: CreateVerificationToken :one
INSERT INTO mail_verifications (
  user_id, token, expires_at
) VALUES (
  $1, $2, $3
)
RETURNING *;

-- name: DeleteVerificationToken :exec
DELETE FROM mail_verifications
WHERE token = $1;

-- name: DeleteVerificationTokensByUserID :exec
DELETE FROM mail_verifications
WHERE user_id = $1;

-- name: GetUserSettings :one
SELECT * FROM settings
WHERE user_id = $1
LIMIT 1;

-- name: UpdateUserSettings :exec
INSERT INTO settings (user_id, cards_per_day, study_direction, scratch_pad_enabled, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id) 
DO UPDATE SET 
    cards_per_day = $2,
    study_direction = $3,
    scratch_pad_enabled = $4,
    updated_at = NOW();

-- name: CreateUserSettings :one
INSERT INTO settings (
  user_id
) VALUES (
  $1
)
RETURNING *;


-- name: GetUserCard :one
SELECT * FROM user_cards
WHERE user_id = $1 AND card_id = $2
LIMIT 1;

-- name: CreateUserCard :one
INSERT INTO user_cards (
  user_id, card_id
) VALUES (
  $1, $2
)
RETURNING *;

-- name: GetUserCards :many
SELECT * FROM user_cards
WHERE user_id = $1
ORDER BY next_review_at ASC;

-- name: GetUserCardsDueForReview :many
SELECT * FROM user_cards
WHERE user_id = $1 AND next_review_at <= NOW()
ORDER BY next_review_at ASC;

-- name: GetUserCardsDueForReviewCount :one
SELECT COUNT(*) FROM user_cards
WHERE user_id = $1 AND next_review_at <= NOW();

-- name: GetCardsDueForReviewAfter :many
-- The queue selects from the readable-Decks rule, so a Deck the caller may no
-- longer read — deleted included — takes its Cards out of the queue. The Deck
-- filter is applied here rather than to the rows afterwards; a zero filter means
-- every readable Deck.
SELECT c.* FROM cards c
JOIN readable_decks(sqlc.arg(user_id)::bigint) d ON d.id = c.deck_id
WHERE c.id IN (
    SELECT uc.card_id FROM user_cards uc
    WHERE uc.user_id = sqlc.arg(user_id)
      AND uc.next_review_at <= NOW()
      AND uc.next_review_at > sqlc.arg(next_review_at)
  )
  AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (sqlc.arg(deck_id)::bigint = 0 OR c.deck_id = sqlc.arg(deck_id));

-- name: GetCardsDueForReview :many
-- The due queue selects from the same rule as every other Deck-scoped read, with
-- the same Deck filter: a zero filter means every readable Deck.
SELECT c.* FROM cards c
JOIN readable_decks(sqlc.arg(user_id)::bigint) d ON d.id = c.deck_id
WHERE c.id IN (
    SELECT uc.card_id FROM user_cards uc
    WHERE uc.user_id = sqlc.arg(user_id) AND uc.next_review_at <= NOW()
  )
  AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (sqlc.arg(deck_id)::bigint = 0 OR c.deck_id = sqlc.arg(deck_id));

-- name: ListUserCardSchedules :many
-- The caller's own Review schedules for a set of Cards, so a Card response can
-- carry what the device needs to compute forward.
SELECT * FROM user_cards
WHERE user_id = $1 AND card_id = ANY(sqlc.arg(card_ids)::bigint[]);

-- name: ApplyGrade :execrows
-- Stores the Review schedule the device computed, whole: repetitions and
-- last_reviewed_at included. A Grade the device answered no later than the one
-- already stored is a replay and changes nothing, so the later answer wins.
INSERT INTO user_cards (
  user_id, card_id, efactor, interval, repetitions, last_reviewed_at, next_review_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (user_id, card_id)
DO UPDATE SET
  efactor = EXCLUDED.efactor,
  interval = EXCLUDED.interval,
  repetitions = EXCLUDED.repetitions,
  last_reviewed_at = EXCLUDED.last_reviewed_at,
  next_review_at = EXCLUDED.next_review_at,
  updated_at = NOW()
WHERE user_cards.last_reviewed_at IS NULL
   OR user_cards.last_reviewed_at < EXCLUDED.last_reviewed_at;


-- name: UpdateUserCard :exec
UPDATE user_cards
SET efactor = $3,
    interval = $4,
    repetitions = $5,
    last_reviewed_at = $6,
    next_review_at = $7,
    updated_at = NOW()
WHERE user_id = $1 AND card_id = $2;

-- name: DeleteUserCard :exec
DELETE FROM user_cards
WHERE user_id = $1 AND card_id = $2;

-- name: GetCard :one
-- A Card the listings no longer carry is read as gone: the paths that settle one
-- Card by its id read it through this.
SELECT * FROM cards
WHERE id = $1 AND (deleted_at > NOW() OR deleted_at IS NULL)
LIMIT 1;

-- name: ListAccessibleCards :many
-- The cross-Deck listing selects from the readable-Decks rule, so a deleted Deck
-- takes its Cards with it and no query re-spells public / owner / shared.
SELECT c.* FROM cards c
JOIN readable_decks(sqlc.narg(user_id)::bigint) d ON d.id = c.deck_id
WHERE (c.deleted_at > NOW() OR c.deleted_at IS NULL)
ORDER BY c.id ASC;

-- name: ListAccessibleCardsPaged :many
SELECT c.* FROM cards c
JOIN readable_decks(sqlc.narg(user_id)::bigint) d ON d.id = c.deck_id
WHERE (c.deleted_at > NOW() OR c.deleted_at IS NULL)
ORDER BY c.id ASC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ListCardsByDeck :many
-- A Deck-scoped listing is a single-Deck read, so it applies the same liveness
-- rule as GetDeck: a deleted Deck has no Cards to list.
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id AND d.deleted_at IS NULL
WHERE c.deck_id = $1 AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
ORDER BY c.id ASC
LIMIT $2 OFFSET $3;

-- name: ListCardsByDeckAll :many
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id AND d.deleted_at IS NULL
WHERE c.deck_id = $1 AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
ORDER BY c.id ASC;

-- name: CreateCard :one
INSERT INTO cards (
  deck_id, korean_word, english_word, context, example_sentence
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING *;

-- name: UpdateCard :exec
UPDATE cards
SET deck_id = $2,
    korean_word = $3,
    english_word = $4,
    context = $5,
    example_sentence = $6,
    updated_at = NOW()
WHERE id = $1;

-- name: DeleteCard :exec
UPDATE cards
SET deleted_at = NOW()
WHERE id = $1;

-- name: GetDeck :one
SELECT * FROM decks
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: ListDecks :many
SELECT * FROM decks
WHERE deleted_at IS NULL
ORDER BY name;

-- name: ListReadableDecks :many
-- The Deck listing and every Deck-scoped read take their rows and roles from the
-- one readable-Decks rule. A NULL caller is anonymous. The join to decks is what
-- gives sqlc the row's column types; readable_decks decides which rows join and
-- the role they carry.
SELECT d.*, r.access_role::text AS access_role
FROM decks d
JOIN readable_decks(sqlc.narg(caller)::bigint) r ON r.id = d.id
ORDER BY d.name;

-- name: CreateDeck :one
INSERT INTO decks (
  name, type, owner_id, is_public, share_token
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING *;

-- name: UpdateDeck :exec
UPDATE decks
SET name = $2,
    type = $3,
    is_public = $4,
    updated_at = NOW()
WHERE id = $1;

-- name: DeleteDeck :exec
UPDATE decks
SET deleted_at = NOW()
WHERE id = $1;

-- name: SetDeckShareToken :exec
UPDATE decks
SET share_token = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: GetDeckByShareToken :one
SELECT * FROM decks
WHERE share_token = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: CreateDeckShare :one
INSERT INTO deck_shares (
  deck_id, user_id, invited_email, permission
) VALUES (
  $1, $2, $3, $4
)
ON CONFLICT (deck_id, user_id)
DO UPDATE SET
  permission = $4,
  updated_at = NOW()
RETURNING *;

-- name: GetDeckShare :one
SELECT * FROM deck_shares
WHERE deck_id = $1 AND user_id = $2
LIMIT 1;

-- name: DeleteDeckShare :exec
DELETE FROM deck_shares
WHERE deck_id = $1 AND user_id = $2;

-- name: ListDeckShares :many
SELECT * FROM deck_shares
WHERE deck_id = $1
ORDER BY created_at ASC;

