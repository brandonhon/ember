-- +goose Up
-- +goose StatementBegin
-- Email was previously unconstrained, so two accounts could share an address
-- (digest cross-delivery, account confusion). Null out any pre-existing dupes —
-- keeping the lowest user id — so the unique index below can be created on
-- databases that already accumulated them. Email is digest-delivery only, so a
-- cleared dup just means that account must re-enter a unique address.
UPDATE users SET email = NULL
WHERE email IS NOT NULL AND email != ''
  AND id NOT IN (
    SELECT MIN(id) FROM users
    WHERE email IS NOT NULL AND email != ''
    GROUP BY LOWER(email)
  );
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique
  ON users (email COLLATE NOCASE)
  WHERE email IS NOT NULL AND email != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_email_unique;
-- +goose StatementEnd
