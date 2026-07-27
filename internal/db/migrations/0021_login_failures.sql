-- +goose Up
-- +goose StatementBegin
-- Consecutive failed password logins, keyed on the username exactly as it was
-- submitted. Rows exist for usernames that don't correspond to a real account
-- too: throttling the submitted string rather than a resolved user ID is what
-- keeps the response indistinguishable between "wrong password" and "no such
-- user", so the throttle itself can't be used as an enumeration oracle.
--
-- A row is deleted on the next successful login for that username, and stale
-- rows are reaped by the hourly cleanup tick, so this table stays tiny.
CREATE TABLE login_failures (
  username     TEXT    PRIMARY KEY,
  fails        INTEGER NOT NULL DEFAULT 0,
  last_fail_at INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS login_failures;
-- +goose StatementEnd
