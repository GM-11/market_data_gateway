package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func LoadJWKS(ctx context.Context, source string) (map[string]ed25519.PublicKey, error) {
	data, err := fetch(ctx, source, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	keys, err := parseJWKS(data)
	if err != nil {
		return nil, fmt.Errorf("parse JWKS: %w", err)
	}
	return keys, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	X   string `json:"x"`
}

func parseJWKS(data []byte) (map[string]ed25519.PublicKey, error) {
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	keys := make(map[string]ed25519.PublicKey)
	for _, key := range set.Keys {
		if key.Kty != "OKP" || key.Crv != "Ed25519" || (key.Use != "" && key.Use != "sig") || (key.Alg != "" && key.Alg != "EdDSA") {
			continue
		}
		if key.Kid == "" {
			return nil, fmt.Errorf("JWKS key has empty kid")
		}
		if _, exists := keys[key.Kid]; exists {
			return nil, fmt.Errorf("duplicate JWKS kid %q", key.Kid)
		}
		raw, err := base64.RawURLEncoding.DecodeString(key.X)
		if err != nil {
			return nil, fmt.Errorf("decode JWKS key %q: %w", key.Kid, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("JWKS key %q has invalid public key length %d", key.Kid, len(raw))
		}
		keys[key.Kid] = ed25519.PublicKey(raw)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("JWKS contains no usable Ed25519 keys")
	}
	return keys, nil
}

type JWTVerifier struct {
	keys   map[string]ed25519.PublicKey
	parser *jwt.Parser
}

const maxTokenLifetime = time.Hour

func NewJWTVerifier(keys map[string]ed25519.PublicKey, issuer, audience string, leeway time.Duration, now func() time.Time) *JWTVerifier {
	if now == nil {
		now = time.Now
	}
	return &JWTVerifier{
		keys: keys,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{"EdDSA"}),
			jwt.WithIssuer(issuer),
			jwt.WithAudience(audience),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(leeway),
			jwt.WithTimeFunc(now),
			jwt.WithIssuedAt(),
		),
	}
}

func (v *JWTVerifier) Verify(c Credentials) (Principal, error) {
	if c.Bearer == "" {
		return Principal{}, ErrNoCredentials
	}
	if len(c.Bearer) > 4096 {
		return Principal{}, ErrBadCredentials
	}

	var claims jwt.RegisteredClaims
	_, err := v.parser.ParseWithClaims(c.Bearer, &claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key, ok := v.keys[kid]
		if !ok {
			return nil, jwt.ErrTokenUnverifiable
		}
		return key, nil
	})
	if err != nil || claims.IssuedAt == nil || claims.NotBefore == nil {
		return Principal{}, ErrBadCredentials
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > maxTokenLifetime {
		return Principal{}, ErrBadCredentials
	}

	account, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || account == 0 {
		return Principal{}, ErrBadCredentials
	}
	return Principal{Account: AccountID(account), Method: MethodBrowser}, nil
}
