// Package push wires Ember to the Web Push (VAPID) protocol. The
// keypair is generated on first server start and persisted to the
// app_settings KV; outbound push fan-out lives in notify.go.
package push

import (
	"context"
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// KeyStore is the minimum store surface the VAPID loader needs. The
// real implementation in internal/store satisfies it.
type KeyStore interface {
	GetAppSetting(ctx context.Context, key string) (string, error)
	PutAppSetting(ctx context.Context, key, value string) error
}

const (
	settingPublicKey  = "vapid_public_key"
	settingPrivateKey = "vapid_private_key"
)

// Keys is the resolved VAPID keypair. PublicKey is the base64url string
// the SPA needs to call pushManager.subscribe; PrivateKey is held only
// server-side and used to sign outbound pushes.
type Keys struct {
	PublicKey  string
	PrivateKey string
}

// LoadOrCreateKeys returns the persisted VAPID keypair, generating and
// persisting a fresh one on first call. Rotating the keypair would
// invalidate every existing subscription — so we never auto-rotate; the
// only way to spin a new pair is to delete the rows manually.
func LoadOrCreateKeys(ctx context.Context, store KeyStore) (Keys, error) {
	pub, err := store.GetAppSetting(ctx, settingPublicKey)
	if err != nil {
		return Keys{}, fmt.Errorf("vapid: load public: %w", err)
	}
	priv, err := store.GetAppSetting(ctx, settingPrivateKey)
	if err != nil {
		return Keys{}, fmt.Errorf("vapid: load private: %w", err)
	}
	if pub != "" && priv != "" {
		return Keys{PublicKey: pub, PrivateKey: priv}, nil
	}
	newPriv, newPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("vapid: generate: %w", err)
	}
	if err := store.PutAppSetting(ctx, settingPublicKey, newPub); err != nil {
		return Keys{}, fmt.Errorf("vapid: persist public: %w", err)
	}
	if err := store.PutAppSetting(ctx, settingPrivateKey, newPriv); err != nil {
		return Keys{}, fmt.Errorf("vapid: persist private: %w", err)
	}
	return Keys{PublicKey: newPub, PrivateKey: newPriv}, nil
}
