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
UPDATE users
  set name = $2,
  email = $3,
  password_hash = $4,
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
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id
WHERE c.id IN (
    SELECT uc.card_id FROM user_cards uc
    WHERE uc.user_id = $1 AND uc.next_review_at <= NOW() AND uc.next_review_at > $2
  )
  AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (
    d.is_public = TRUE
    OR d.owner_id = $1
    OR EXISTS (
      SELECT 1 FROM deck_shares s
      WHERE s.deck_id = d.id AND s.user_id = $1
    )
  );

-- name: GetCardsDueForReview :many
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id
WHERE c.id IN (
    SELECT uc.card_id FROM user_cards uc
    WHERE uc.user_id = $1 AND uc.next_review_at <= NOW()
  )
  AND (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (
    d.is_public = TRUE
    OR d.owner_id = $1
    OR EXISTS (
      SELECT 1 FROM deck_shares s
      WHERE s.deck_id = d.id AND s.user_id = $1
    )
  );

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
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id
WHERE (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (
    d.is_public = TRUE
    OR d.owner_id = sqlc.arg(user_id)
    OR EXISTS (
      SELECT 1 FROM deck_shares s
      WHERE s.deck_id = d.id AND s.user_id = sqlc.arg(user_id)
    )
  )
ORDER BY c.id ASC;

-- name: ListAccessibleCardsPaged :many
SELECT c.* FROM cards c
JOIN decks d ON d.id = c.deck_id
WHERE (c.deleted_at > NOW() OR c.deleted_at IS NULL)
  AND (
    d.is_public = TRUE
    OR d.owner_id = sqlc.arg(user_id)
    OR EXISTS (
      SELECT 1 FROM deck_shares s
      WHERE s.deck_id = d.id AND s.user_id = sqlc.arg(user_id)
    )
  )
ORDER BY c.id ASC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ListCardsByDeck :many
SELECT * FROM cards
WHERE deck_id = $1 AND (deleted_at > NOW() OR deleted_at IS NULL)
ORDER BY id ASC
LIMIT $2 OFFSET $3;

-- name: ListCardsByDeckAll :many
SELECT * FROM cards
WHERE deck_id = $1 AND (deleted_at > NOW() OR deleted_at IS NULL)
ORDER BY id ASC;

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

-- name: ListPublicDecks :many
SELECT * FROM decks
WHERE is_public = TRUE AND deleted_at IS NULL
ORDER BY name;

-- name: ListDecksForUser :many
SELECT DISTINCT d.id, d.name, d.type, d.owner_id, d.is_public, d.share_token, d.created_at, d.updated_at, d.deleted_at,
  CASE
    WHEN d.owner_id = $1 THEN 'owner'
    WHEN s.permission IS NOT NULL THEN s.permission
    ELSE 'public'
  END AS user_access_role
FROM decks d
LEFT JOIN deck_shares s ON d.id = s.deck_id AND s.user_id = $1
WHERE d.deleted_at IS NULL AND (
    d.is_public = TRUE
    OR d.owner_id = $1
    OR s.user_id = $1
)
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

