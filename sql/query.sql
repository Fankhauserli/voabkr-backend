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

--- name: GetUserSettings :one
SELECT * FROM settings
WHERE user_id = $1
LIMIT 1;

-- name: UpdateUserSettings :exec
UPDATE settings
SET cards_per_day = $2,
    updated_at = NOW()
WHERE user_id = $1;

--- name: GetUserCard :one
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

-- name: GetUserCardsDueForReviewAfter :many
SELECT * FROM user_cards
WHERE user_id = $1 AND next_review_at <= NOW() AND next_review_at > $2
ORDER BY next_review_at ASC;

-- name: GetCardsDueForReview :many
SELECT * FROM cards
WHERE id IN
(SELECT card_id FROM user_cards
WHERE user_id = $1 AND next_review_at <= NOW());


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
SELECT * FROM cards
WHERE id = $1
LIMIT 1;

-- name: ListCards :many
SELECT * FROM cards
WHERE deleted_at > NOW() OR deleted_at IS NULL;

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
WHERE id = $1
LIMIT 1;

-- name: ListDecks :many
SELECT * FROM decks
WHERE deleted_at > NOW()
ORDER BY name;

-- name: CreateDeck :one
INSERT INTO decks (
  name, type
) VALUES (
  $1, $2
)
RETURNING *;

-- name: UpdateDeck :exec
UPDATE decks
SET name = $2,
    type = $3,
    updated_at = NOW()
WHERE id = $1;

-- name: DeleteDeck :exec
UPDATE decks
SET deleted_at = NOW()
WHERE id = $1;
