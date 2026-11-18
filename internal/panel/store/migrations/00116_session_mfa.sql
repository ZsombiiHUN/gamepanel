-- +goose Up
-- Whether a session's sign-in used two factors, for panels that require
-- them (GAMEPANEL_REQUIRE_MFA). Existing sessions count when their recorded
-- method had two factors, or when the account has an authenticator (sign-in
-- has asked for its code since it was turned on).
ALTER TABLE sessions ADD COLUMN mfa INTEGER NOT NULL DEFAULT 0;
UPDATE sessions SET mfa = 1
 WHERE method IN ('password+totp', 'password+recovery', 'passkey')
    OR method LIKE 'sso+totp%'
    OR user_id IN (SELECT id FROM users WHERE totp_enabled = 1);

-- +goose Down
ALTER TABLE sessions DROP COLUMN mfa;
