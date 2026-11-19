// Package auth checks the bearer token on a share: an auth.romaine.life machine token
// for one of Nelson's home PCs.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer and Audience are both auth.romaine.life's base URL on its machine tokens.
	Issuer   = "https://auth.romaine.life"
	Audience = "https://auth.romaine.life"
	// JWKSURL publishes the RS256 keys auth.romaine.life signs with.
	JWKSURL = "https://auth.romaine.life/api/auth/jwks"

	// A token naming a key the cached set lacks fetches the set again, but not more than
	// once in this long: a stream of tokens with made-up key ids can't turn into a stream
	// of fetches from auth.romaine.life.
	refetchFloor = 10 * time.Second
	leeway       = 30 * time.Second
)

// homePCs are the machines that may share a window: the device claim of their
// auth.romaine.life machine tokens.
var homePCs = map[string]bool{
	"nelsonpc":     true,
	"upstairspc":   true,
	"treadmill":    true,
	"nelsonlaptop": true,
}

// Error is a refusal with its HTTP status and a sentence for the caller.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

var errKeysUnavailable = errors.New("auth.romaine.life's keys could not be fetched")

// Verifier checks machine tokens against auth.romaine.life's published keys. It fetches
// the key set the first time it needs it and again only when a token names a key id the
// set it holds lacks (a rotation), never on a timer.
type Verifier struct {
	jwksURL string
	client  *http.Client
	now     func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// New returns a Verifier reading keys from jwksURL (JWKSURL in production). now is the
// clock, time.Now when nil.
func New(jwksURL string, client *http.Client, now func() time.Time) *Verifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &Verifier{jwksURL: jwksURL, client: client, now: now}
}

type machineClaims struct {
	jwt.RegisteredClaims
	Purpose string `json:"purpose"`
	Device  string `json:"device"`
}

// Device checks token and returns the home PC it was minted for. A refusal is an *Error.
func (v *Verifier) Device(ctx context.Context, token string) (string, error) {
	claims := &machineClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(leeway),
		jwt.WithTimeFunc(v.now),
	)
	_, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token names no key id")
		}
		return v.key(ctx, kid)
	})
	switch {
	case errors.Is(err, errKeysUnavailable):
		return "", &Error{http.StatusServiceUnavailable, "The service could not fetch auth.romaine.life's signing keys; try again shortly."}
	case errors.Is(err, jwt.ErrTokenExpired):
		return "", &Error{http.StatusUnauthorized, "The token has expired; mint a new machine token."}
	case err != nil:
		return "", &Error{http.StatusUnauthorized, "The token is not a valid auth.romaine.life token."}
	}
	if claims.Purpose != "machine" {
		return "", &Error{http.StatusForbidden, "Only an auth.romaine.life machine token can share a window."}
	}
	if claims.Device == "" {
		return "", &Error{http.StatusForbidden, "The machine token names no device."}
	}
	if !homePCs[claims.Device] {
		return "", &Error{http.StatusForbidden, fmt.Sprintf("%s is not one of the home PCs that can share a window.", claims.Device)}
	}
	return claims.Device, nil
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	if v.keys != nil && v.now().Sub(v.fetched) < refetchFloor {
		return nil, fmt.Errorf("unknown key id %q", kid)
	}
	keys, err := v.fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errKeysUnavailable, err)
	}
	v.keys, v.fetched = keys, v.now()
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS answered HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("JWKS is not JSON: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	return keys, nil
}
