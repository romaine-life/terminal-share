// Package testissuer stands in for auth.romaine.life: it signs machine tokens with its own
// RSA keys and serves them as a JWKS. The tests and the image's self-test use it; nothing
// in the running service does.
package testissuer

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Key is one signing key and the id the JWKS publishes it under.
type Key struct {
	KID     string
	private *rsa.PrivateKey
}

func NewKey(kid string) (*Key, error) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Key{KID: kid, private: private}, nil
}

// Sign returns claims as an RS256 token naming this key.
func (k *Key) Sign(claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = k.KID
	signed, err := token.SignedString(k.private)
	if err != nil {
		panic(err)
	}
	return signed
}

// Machine is the claims of an auth.romaine.life machine token for device, as one minted
// by `romaine-auth token` carries them, issued at now and valid for 15 minutes.
func Machine(device string, now time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"sub":         "device-sub",
		"email":       "machine@service.machine.romaine.life",
		"name":        "Machine: " + device,
		"role":        "service",
		"apps":        map[string]any{},
		"iat":         now.Unix(),
		"exp":         now.Add(15 * time.Minute).Unix(),
		"actor_email": "nelson@romaine.life",
		"purpose":     "machine",
		"device":      device,
		"iss":         "https://auth.romaine.life",
		"aud":         "https://auth.romaine.life",
	}
}

// JWKS serves the public halves of its keys and counts the fetches.
type JWKS struct {
	mu      sync.Mutex
	keys    []*Key
	fetches int
}

func NewJWKS(keys ...*Key) *JWKS { return &JWKS{keys: keys} }

// SetKeys replaces the published keys (a rotation).
func (j *JWKS) SetKeys(keys ...*Key) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.keys = keys
}

func (j *JWKS) Fetches() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.fetches
}

func (j *JWKS) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	j.mu.Lock()
	j.fetches++
	type jwk struct {
		Alg string `json:"alg"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
		Kid string `json:"kid"`
	}
	set := struct {
		Keys []jwk `json:"keys"`
	}{Keys: []jwk{}}
	for _, k := range j.keys {
		set.Keys = append(set.Keys, jwk{
			Alg: "RS256",
			Kty: "RSA",
			N:   base64.RawURLEncoding.EncodeToString(k.private.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.private.E)).Bytes()),
			Kid: k.KID,
		})
	}
	j.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}
