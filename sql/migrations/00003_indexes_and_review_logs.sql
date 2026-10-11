-- +goose Up
CREATE INDEX IF NOT EXISTS idx_user_cards_due ON user_cards (user_id, next_review_at)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_cards_deck ON cards (deck_id)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_cards_deleted_at ON cards (deleted_at);
CREATE INDEX IF NOT EXISTS idx_decks_deleted_at ON decks (deleted_at);

CREATE TABLE IF NOT EXISTS review_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    rating INT NOT NULL,
    interval INT NOT NULL,
    efactor FLOAT NOT NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_review_logs_user_date ON review_logs (user_id, reviewed_at);

-- +goose Down
DROP TABLE IF EXISTS review_logs;
DROP INDEX IF EXISTS idx_review_logs_user_date;
DROP INDEX IF EXISTS idx_decks_deleted_at;
DROP INDEX IF EXISTS idx_cards_deleted_at;
DROP INDEX IF EXISTS idx_cards_deck;
DROP INDEX IF EXISTS idx_user_cards_due;
