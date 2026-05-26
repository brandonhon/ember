## What

<!-- Brief description of the change. Link to related issues. -->

## Why

<!-- Motivation. Why is this change needed? -->

## How

<!-- Notable implementation choices. Skip if obvious from the diff. -->

## Checklist

- [ ] Tests added or updated (Go + Playwright as appropriate)
- [ ] `make verify` passes locally
- [ ] `make web-check` passes (if SPA touched)
- [ ] Conventional commit prefix (`feat`, `fix`, `chore`, `docs`, `sec`, `refactor`, `style`, `test`)
- [ ] No new `panic()` calls in handlers or stores
- [ ] User-scoped DB queries include `WHERE user_id = ?`
- [ ] Migrations append-only (no edits to committed `.sql` files)
- [ ] Docs updated if behavior or env changed
