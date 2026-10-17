-- +goose Up

-- readable_decks answers the one question every Deck-scoped read asks: which live
-- Decks may this caller read, and under which Access role? A NULL or zero caller
-- is anonymous, answered with the public Decks alone. The role strings are the
-- wire vocabulary ADR-0004 fixes.
-- +goose StatementBegin
CREATE FUNCTION readable_decks(caller BIGINT)
RETURNS TABLE (
    id BIGINT,
    name TEXT,
    type deck_type,
    owner_id BIGINT,
    is_public BOOLEAN,
    share_token VARCHAR(64),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    access_role TEXT
)
LANGUAGE sql
STABLE
AS $$
    -- The role is resolved once; readability is then read off it, so the
    -- public / owner / shared rule is not spelled a second time in a filter.
    WITH readable AS (
        SELECT
            d.id,
            d.name,
            d.type,
            d.owner_id,
            d.is_public,
            d.share_token,
            d.created_at,
            d.updated_at,
            d.deleted_at,
            CASE
                WHEN caller IS NULL OR caller = 0 THEN 'public'
                WHEN d.owner_id = caller THEN 'owner'
                WHEN s.permission IS NOT NULL AND s.permission <> '' THEN s.permission
                ELSE 'public'
            END AS access_role
        FROM decks d
        LEFT JOIN deck_shares s
            ON s.deck_id = d.id
           AND s.user_id = caller
        WHERE d.deleted_at IS NULL
    )
    SELECT * FROM readable
    WHERE is_public OR access_role <> 'public';
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS readable_decks(BIGINT);
