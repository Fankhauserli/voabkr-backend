-- +goose Up

-- 1. Add ownership and visibility to decks
ALTER TABLE decks 
    ADD COLUMN IF NOT EXISTS owner_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS share_token VARCHAR(64) UNIQUE;

-- Existing system decks remain system-owned (owner_id = NULL) and public
UPDATE decks SET is_public = TRUE WHERE owner_id IS NULL;

-- 2. Create Deck Sharing / Collaborator Table
CREATE TABLE IF NOT EXISTS deck_shares (
    id BIGSERIAL PRIMARY KEY,
    deck_id BIGINT NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    invited_email TEXT,
    permission VARCHAR(20) NOT NULL DEFAULT 'view', -- 'view' (study only) | 'edit' (collaborative)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(deck_id, user_id)
);

-- 3. Password Resets table for forgot-password flow
CREATE TABLE IF NOT EXISTS password_resets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- 4. Indexes for high-performance access checks
CREATE INDEX IF NOT EXISTS idx_decks_owner_id ON decks(owner_id);
CREATE INDEX IF NOT EXISTS idx_decks_is_public ON decks(is_public);
CREATE INDEX IF NOT EXISTS idx_decks_share_token ON decks(share_token);
CREATE INDEX IF NOT EXISTS idx_deck_shares_user_id ON deck_shares(user_id);
CREATE INDEX IF NOT EXISTS idx_deck_shares_deck_id ON deck_shares(deck_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token);
CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets(user_id);

-- +goose Down
DROP TABLE IF EXISTS password_resets;
DROP TABLE IF EXISTS deck_shares;
ALTER TABLE decks DROP COLUMN IF EXISTS share_token;
ALTER TABLE decks DROP COLUMN IF EXISTS is_public;
ALTER TABLE decks DROP COLUMN IF EXISTS owner_id;
