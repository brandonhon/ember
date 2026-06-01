package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/brandonhon/ember/internal/push"
)

// PushSubscription is the public row shape: includes per-row metadata the
// UI lists (user_agent, created_at) plus the id. The endpoint / p256dh /
// auth fields are present so notify.go can convert via PushPair, but
// are not exposed by the API list response.
type PushSubscription struct {
	ID        int64  `json:"id"`
	Endpoint  string `json:"-"`
	P256dh    string `json:"-"`
	Auth      string `json:"-"`
	UserAgent string `json:"user_agent"`
	CreatedAt int64  `json:"created_at"`
}

// CreatePushSubscription inserts a new browser subscription for the
// user. Duplicate endpoints (same browser re-subscribing) update the
// existing row's user_agent and return the existing id.
func (s *Store) CreatePushSubscription(ctx context.Context, userID int64, endpoint, p256dh, auth, userAgent string) (int64, error) {
	if endpoint == "" || p256dh == "" || auth == "" {
		return 0, errors.New("push: endpoint, p256dh, auth all required")
	}
	now := s.nowUnix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			user_id    = excluded.user_id,
			p256dh     = excluded.p256dh,
			auth       = excluded.auth,
			user_agent = excluded.user_agent`,
		userID, endpoint, p256dh, auth, userAgent, now)
	if err != nil {
		return 0, fmt.Errorf("push: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("push: last insert id: %w", err)
	}
	if id == 0 {
		// ON CONFLICT path — fetch the existing id.
		if err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM push_subscriptions WHERE endpoint = ?`, endpoint).Scan(&id); err != nil {
			return 0, fmt.Errorf("push: refetch id: %w", err)
		}
	}
	return id, nil
}

// ListPushSubscriptions returns the rows for a user, hiding the
// cryptographic fields (callers that need them use
// ListSubscriptionsForUser).
func (s *Store) ListPushSubscriptions(ctx context.Context, userID int64) ([]PushSubscription, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, IFNULL(user_agent,''), created_at
		FROM push_subscriptions
		WHERE user_id = ?
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("push: list: %w", err)
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserAgent, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("push: scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: iterate: %w", err)
	}
	if out == nil {
		out = []PushSubscription{}
	}
	return out, nil
}

// ListSubscriptionsForUser is the notifier-facing variant that returns
// the full row including crypto fields. Satisfies push.SubStore.
func (s *Store) ListSubscriptionsForUser(ctx context.Context, userID int64) ([]push.Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, endpoint, p256dh, auth
		FROM push_subscriptions
		WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("push: notify list: %w", err)
	}
	defer rows.Close()
	var out []push.Subscription
	for rows.Next() {
		var sub push.Subscription
		if err := rows.Scan(&sub.ID, &sub.Endpoint, &sub.P256dh, &sub.Auth); err != nil {
			return nil, fmt.Errorf("push: notify scan: %w", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: notify iterate: %w", err)
	}
	return out, nil
}

// DeletePushSubscription removes a row owned by userID. Returns
// ErrNotFound when the row doesn't exist or belongs to someone else
// (the auth layer doesn't expose that distinction to the client).
func (s *Store) DeletePushSubscription(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("push: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("push: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubscriptionByEndpoint drops a subscription regardless of user.
// Used by the notifier when the push service returns 404/410 — we don't
// know which user we're deleting for, only the endpoint.
func (s *Store) DeleteSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	if err != nil {
		return fmt.Errorf("push: delete by endpoint: %w", err)
	}
	return nil
}
