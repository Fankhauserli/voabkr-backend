-- +goose Up

-- The starter Decks were seeded with a deletion moment far in the future, which
-- every live-Deck read treats as deleted: they carry deleted_at even though
-- their deletion has not happened. Restore the ownerless Decks whose deletion
-- moment is still ahead; learner-owned Decks and deletions that have already
-- taken effect are untouched. The statement is idempotent.
UPDATE decks
SET deleted_at = NULL
WHERE owner_id IS NULL
  AND deleted_at > NOW();

-- +goose Down

-- The original deletion moment is not recorded, so the stamp cannot be put back.
