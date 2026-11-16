-- +goose Up
-- A server's activity log reads the audit log by target.
CREATE INDEX audit_log_target ON audit_log(target, id DESC);

-- +goose Down
DROP INDEX audit_log_target;
