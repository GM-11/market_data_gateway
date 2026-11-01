package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"sync/atomic"
)

type apiKey struct {
	account  AccountID
	hash     [32]byte // SHA-256 of the secret
	disabled bool
}

type keyTable struct {
	version uint64
	byID    map[string]apiKey
}

type APIKeys struct {
	current atomic.Pointer[keyTable] // nil until the first successful Refresh
	dummy   [32]byte                 // compared against when the key id is unknown
	log     *slog.Logger
}

func (k *APIKeys) Verify(c Credentials) (Principal, error) {
	if c.KeyID == "" && c.Secret == "" {
		return Principal{}, ErrNoCredentials
	}
	if c.KeyID == "" || c.Secret == "" {
		return Principal{}, ErrBadCredentials
	}

	if len(c.KeyID) > 32 || len(c.Secret) > 128 {
		return Principal{}, ErrBadCredentials
	}

	got := sha256.Sum256([]byte(c.Secret))

	e, ok := k.current.Load().byID[c.KeyID]
	want := e.hash
	if !ok {
		want = k.dummy
	}
	same := subtle.ConstantTimeCompare(got[:], want[:]) == 1

	if !ok || !same || e.disabled {
		return Principal{}, ErrBadCredentials
	}

	return Principal{
		Account: e.account,
		Method:  MethodAPIKey,
		KeyID:   c.KeyID,
	}, nil
}
