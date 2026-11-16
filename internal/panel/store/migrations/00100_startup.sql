-- +goose Up
-- Every image an egg offers, in the egg's order: [{"label":..., "image":...}].
-- The first one is the default (docker_image keeps a copy of it).
ALTER TABLE eggs ADD COLUMN docker_images TEXT NOT NULL DEFAULT '[]';
UPDATE eggs SET docker_images = json_array(json_object('label', docker_image, 'image', docker_image));

-- Variables imported before this release had no visibility settings: keep
-- them visible and editable, as they were.
UPDATE eggs SET variables = (
  SELECT COALESCE(json_group_array(json_set(value, '$.user_viewable', json('true'), '$.user_editable', json('true'), '$.rules', '')), '[]')
  FROM json_each(eggs.variables)
);

-- Per-server choices: '' means "use the egg's".
ALTER TABLE servers ADD COLUMN image   TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN startup TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN startup;
ALTER TABLE servers DROP COLUMN image;
ALTER TABLE eggs DROP COLUMN docker_images;
