-- +goose Up
ALTER TABLE settings 
ADD COLUMN IF NOT EXISTS study_direction text NOT NULL DEFAULT 'koreanToEnglish',
ADD COLUMN IF NOT EXISTS scratch_pad_enabled boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE settings 
DROP COLUMN IF EXISTS scratch_pad_enabled,
DROP COLUMN IF EXISTS study_direction;
