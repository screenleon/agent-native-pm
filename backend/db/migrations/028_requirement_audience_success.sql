-- +migrate Up
ALTER TABLE requirements ADD COLUMN audience TEXT NOT NULL DEFAULT '';
ALTER TABLE requirements ADD COLUMN success_criteria TEXT NOT NULL DEFAULT '';
