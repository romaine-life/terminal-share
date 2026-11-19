package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/romaine-life/terminal-share/internal/testissuer"
)

var (
	keysOnce   sync.Once
	keyA, keyB *testissuer.Key
)

func keys(t *testing.T) (*testissuer.Key, *testissuer.Key) {
	t.Helper()
	keysOnce.Do(func() {
		var err error
		if keyA, err = testissuer.NewKey("key-a"); err != nil {
			panic(err)
		}
		if keyB, err = testissuer.NewKey("key-b"); err != nil {
			panic(err)
		}
	})
	return keyA, keyB
}

// clock is a settable time, so nothing here waits on the real one.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T, published ...*testissuer.Key) (*Verifier, *testissuer.JWKS, *clock) {
	t.Helper()
	jwks := testissuer.NewJWKS(published...)
	srv := httptest.NewServer(jwks)
	t.Cleanup(srv.Close)
	c := &clock{t: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)}
	return New(srv.URL, srv.Client(), c.now), jwks, c
}

func refusal(t *testing.T, err error, status int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("got %v, want a %d refusal", err, status)
	}
}

func TestAcceptsAHomePCsMachineToken(t *testing.T) {
	a, _ := keys(t)
	v, _, c := setup(t, a)
	for _, pc := range []string{"nelsonpc", "upstairspc", "treadmill", "nelsonlaptop"} {
		device, err := v.Device(context.Background(), a.Sign(testissuer.Machine(pc, c.t)))
		if err != nil || device != pc {
			t.Fatalf("%s: got %q, %v", pc, device, err)
		}
	}
}

func TestRefusesTokens(t *testing.T) {
	a, b := keys(t)
	v, _, c := setup(t, a)
	with := func(change func(jwt.MapClaims)) string {
		claims := testissuer.Machine("nelsonpc", c.t)
		change(claims)
		return a.Sign(claims)
	}
	cases := []struct {
		name   string
		token  string
		status int
	}{
		{"garbage", "not-a-token", http.StatusUnauthorized},
		{"unpublished key", b.Sign(testissuer.Machine("nelsonpc", c.t)), http.StatusUnauthorized},
		{"wrong issuer", with(func(m jwt.MapClaims) { m["iss"] = "https://evil.example" }), http.StatusUnauthorized},
		{"wrong audience", with(func(m jwt.MapClaims) { m["aud"] = "https://glimmung.romaine.life" }), http.StatusUnauthorized},
		{"expired", with(func(m jwt.MapClaims) { m["exp"] = c.t.Add(-time.Hour).Unix() }), http.StatusUnauthorized},
		{"no expiry", with(func(m jwt.MapClaims) { delete(m, "exp") }), http.StatusUnauthorized},
		{"a person's token", with(func(m jwt.MapClaims) { delete(m, "purpose"); delete(m, "device") }), http.StatusForbidden},
		{"machine without device", with(func(m jwt.MapClaims) { delete(m, "device") }), http.StatusForbidden},
		{"not a home PC", with(func(m jwt.MapClaims) { m["device"] = "nelson-laptop" }), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Device(context.Background(), tc.token)
			refusal(t, err, tc.status)
		})
	}
}

func TestRefusesAnUnsignedToken(t *testing.T) {
	a, _ := keys(t)
	v, _, c := setup(t, a)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, testissuer.Machine("nelsonpc", c.t))
	token.Header["kid"] = a.KID
	unsigned, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Device(context.Background(), unsigned)
	refusal(t, err, http.StatusUnauthorized)
}

func TestFetchesKeysOnlyForAnUnknownKeyID(t *testing.T) {
	a, b := keys(t)
	v, jwks, c := setup(t, a)
	for range 3 {
		if _, err := v.Device(context.Background(), a.Sign(testissuer.Machine("nelsonpc", c.t))); err != nil {
			t.Fatal(err)
		}
	}
	if jwks.Fetches() != 1 {
		t.Fatalf("fetched the keys %d times for one key id, want 1", jwks.Fetches())
	}

	// auth.romaine.life rotates to key B. A token naming it fetches the set again, but
	// not twice within the floor.
	jwks.SetKeys(a, b)
	c.t = c.t.Add(refetchFloor)
	if _, err := v.Device(context.Background(), b.Sign(testissuer.Machine("nelsonpc", c.t))); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	if jwks.Fetches() != 2 {
		t.Fatalf("fetches = %d after a rotation, want 2", jwks.Fetches())
	}
	forged, _ := testissuer.NewKey("made-up")
	for range 3 {
		_, err := v.Device(context.Background(), forged.Sign(testissuer.Machine("nelsonpc", c.t)))
		refusal(t, err, http.StatusUnauthorized)
	}
	if jwks.Fetches() != 2 {
		t.Fatalf("fetches = %d after made-up key ids within the floor, want 2", jwks.Fetches())
	}
}

func TestUnreachableKeysAreAServiceError(t *testing.T) {
	a, _ := keys(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	v := New(srv.URL, srv.Client(), func() time.Time { return now })
	_, err := v.Device(context.Background(), a.Sign(testissuer.Machine("nelsonpc", now)))
	refusal(t, err, http.StatusServiceUnavailable)
}
