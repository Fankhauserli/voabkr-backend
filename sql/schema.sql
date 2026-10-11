CREATE TABLE users (
    id   BIGSERIAL PRIMARY KEY,
    name text      NOT NULL,
    email text      NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE mail_verifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token text NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE password_resets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token text NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE settings (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cards_per_day INT NOT NULL DEFAULT 20,
    study_direction text NOT NULL DEFAULT 'koreanToEnglish',
    scratch_pad_enabled boolean NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);

CREATE TYPE deck_type AS ENUM (
    'grammar',
    'vocabulary'
);

CREATE TABLE decks (
    id BIGSERIAL PRIMARY KEY,
    name text NOT NULL,
    type deck_type NOT NULL,
    owner_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    share_token VARCHAR(64) UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE deck_shares (
    id BIGSERIAL PRIMARY KEY,
    deck_id BIGINT NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    invited_email TEXT,
    permission VARCHAR(20) NOT NULL DEFAULT 'view',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(deck_id, user_id)
);

CREATE TABLE cards (
    id BIGSERIAL PRIMARY KEY,
    deck_id BIGINT NOT NULL REFERENCES decks(id) ON DELETE CASCADE,
    korean_word text NOT NULL,
    english_word text NOT NULL,
    context text,
    example_sentence text,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE user_cards (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    efactor FLOAT NOT NULL DEFAULT 2.5,
    interval INT NOT NULL DEFAULT 1,
    repetitions INT NOT NULL DEFAULT 1,
    last_reviewed_at TIMESTAMPTZ,
    next_review_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE(user_id, card_id)
);

CREATE INDEX idx_user_cards_due ON user_cards (user_id, next_review_at)
WHERE deleted_at IS NULL;

CREATE INDEX idx_cards_deck ON cards (deck_id)
WHERE deleted_at IS NULL;

CREATE INDEX idx_cards_deleted_at ON cards (deleted_at);
CREATE INDEX idx_decks_deleted_at ON decks (deleted_at);

CREATE INDEX idx_decks_owner_id ON decks(owner_id);
CREATE INDEX idx_decks_is_public ON decks(is_public);
CREATE INDEX idx_decks_share_token ON decks(share_token);
CREATE INDEX idx_deck_shares_user_id ON deck_shares(user_id);
CREATE INDEX idx_deck_shares_deck_id ON deck_shares(deck_id);
CREATE INDEX idx_password_resets_token ON password_resets(token);
CREATE INDEX idx_password_resets_user_id ON password_resets(user_id);

CREATE TABLE review_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id BIGINT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    rating INT NOT NULL,
    interval INT NOT NULL,
    efactor FLOAT NOT NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_review_logs_user_date ON review_logs (user_id, reviewed_at);
