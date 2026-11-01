package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Credentials struct {
	KeyID  string
	Secret string
	Bearer string
}

type Method uint8
type AccountID uint64

const (
	MethodAPIKey Method = iota + 1
	MethodBrowser
)

type Principal struct {
	Account AccountID
	Method  Method
	KeyID   string
}

type Verifier interface {
	Verify(c Credentials) (Principal, error)
}

var (
	ErrNoCredentials  = errors.New("auth: no credentials")
	ErrBadCredentials = errors.New("auth: invalid credentials")
)

func FromRequest(r *http.Request) Credentials {
	h := r.Header.Get("Authorization")
	bearer := ""
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		bearer = h[7:]
	}
	return Credentials{KeyID: r.Header.Get("X-Api-Key-Id"), Secret: r.Header.Get("X-Api-Secret"), Bearer: bearer}
}

type Chain struct {
	APIKeys *APIKeys
	JWT     *JWTVerifier
}

var _ Verifier = Chain{}

func (c Chain) Verify(cr Credentials) (Principal, error) {
	if cr.KeyID != "" || cr.Secret != "" {
		if c.APIKeys == nil {
			return Principal{}, ErrBadCredentials
		}
		return c.APIKeys.Verify(cr)
	}
	if cr.Bearer != "" {
		if c.JWT == nil {
			return Principal{}, ErrBadCredentials
		}
		return c.JWT.Verify(cr)
	}
	return Principal{}, ErrNoCredentials
}

func Middleware(v Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := v.Verify(FromRequest(r))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

var errTooLarge = errors.New("auth: response too large")

func fetch(ctx context.Context, source string, max int64) ([]byte, error) {
	var body io.Reader
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected HTTP status: %s", resp.Status)
		}
		body = resp.Body
	} else {
		file, err := os.Open(strings.TrimPrefix(source, "file://"))
		if err != nil {
			return nil, err
		}
		defer file.Close()
		body = file
	}
	data, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errTooLarge
	}
	return data, nil
}
