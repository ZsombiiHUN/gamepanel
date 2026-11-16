-- +goose Up
-- Sessions made before the 12-hour absolute lifetime (they had 14 days) are
-- cut to at most 12 hours from now, so the new limit holds for everyone
-- within a bounded grace period. A session that ends sooner keeps its end.
UPDATE sessions SET expires_at = MIN(expires_at, CAST(strftime('%s','now') AS INTEGER) + 12*3600);

-- Sessions that exist now may still be held under the old cookie name
-- (gp_session). Over HTTPS that name is accepted once per such session, which
-- is then moved to the __Host- name; sessions made from now on never qualify,
-- so the old name stops working over HTTPS when these sessions end.
ALTER TABLE sessions ADD COLUMN legacy_cookie INTEGER NOT NULL DEFAULT 0;
UPDATE sessions SET legacy_cookie = 1;

-- +goose Down
ALTER TABLE sessions DROP COLUMN legacy_cookie;
