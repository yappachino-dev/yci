-- +goose Up
-- +goose StatementBegin
-- Normalise NULL track durations to 0.
--
-- duration_sec was declared `INTEGER DEFAULT 0`, but 0 and NULL ended up
-- meaning the same thing: a length ffprobe has not reported yet. Both render
-- as the unknown marker, so the difference was invisible, and it was not
-- harmless. `track probe-duration` selects `WHERE duration_sec = 0`, so the
-- NULL rows were skipped entirely: 107 tracks showed as unknown while the
-- probe reported 234 candidates, and the two numbers could never agree.
--
-- 0 is the value the rest of the code already treats as "not yet known", and
-- every write path binds a valid integer, so this converges the data rather
-- than introducing a third state.
UPDATE tracks
SET duration_sec = 0
WHERE duration_sec IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Not reversible: NULL and 0 are the same fact, so there is nothing to
-- restore. Rolling back would invent a distinction that carries no meaning.
SELECT 1;
-- +goose StatementEnd