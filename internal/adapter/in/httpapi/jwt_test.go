package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixedNow is frozen at start-up: validClaims (real time) and the verifier's
// clock then agree to within a test run, and boundary tests stay exact.
var fixedNow = time.Now().Truncate(time.Second)

func fixedClock() time.Time { return fixedNow }

func newVerifier(t *testing.T) *RS256Verifier {
	t.Helper()
	v, err := NewRS256Verifier(publicKeyPEM(testKey), fixedClock)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// claimsAt is validClaims anchored on fixedNow, expiring after d.
func claimsAt(d time.Duration, sub string, roles, perms []string) claimSet {
	c := validClaims(sub, roles, perms)
	c["iat"] = fixedNow.Unix()
	c["exp"] = fixedNow.Add(d).Unix()
	return c
}

func TestVerify_ReturnsTheVerifiedClaims(t *testing.T) {
	v := newVerifier(t)
	c := claimsAt(time.Hour, testSub, []string{"INVENTORY"}, []string{"products:read", "products:write"})

	got, err := v.Verify(rs256Token(c))
	if err != nil {
		t.Fatal(err)
	}
	if got.Sub != testSub || len(got.Roles) != 1 || got.Roles[0] != "INVENTORY" || len(got.Permissions) != 2 {
		t.Errorf("unexpected claims: %+v", got)
	}
}

func TestVerify_ExpiryHonoursTheThirtySecondSkew(t *testing.T) {
	v := newVerifier(t)
	for _, c := range []struct {
		name string
		exp  time.Duration
		ok   bool
	}{
		{"expires in 1 h", time.Hour, true},
		{"expires now", 0, true},
		{"expired 29 s ago", -29 * time.Second, true},
		{"expired 31 s ago", -31 * time.Second, false},
	} {
		tok := rs256Token(claimsAt(c.exp, testSub, []string{"ADMIN"}, nil))
		if _, err := v.Verify(tok); (err == nil) != c.ok {
			t.Errorf("%s: accepted=%v, want %v", c.name, err == nil, c.ok)
		}
	}
}

func TestVerify_IssuedAtAndNotBeforeHonourTheSkew(t *testing.T) {
	v := newVerifier(t)
	base := claimsAt(time.Hour, testSub, nil, nil)
	for _, c := range []struct {
		name   string
		claims claimSet
		ok     bool
	}{
		{"iat 29 s ahead", base.with("iat", fixedNow.Add(29*time.Second).Unix()), true},
		{"iat 31 s ahead", base.with("iat", fixedNow.Add(31*time.Second).Unix()), false},
		{"nbf 29 s ahead", base.with("nbf", fixedNow.Add(29*time.Second).Unix()), true},
		{"nbf 31 s ahead", base.with("nbf", fixedNow.Add(31*time.Second).Unix()), false},
	} {
		if _, err := v.Verify(rs256Token(c.claims)); (err == nil) != c.ok {
			t.Errorf("%s: accepted=%v, want %v", c.name, err == nil, c.ok)
		}
	}
}

func TestVerify_SubjectRulesDependOnWhetherTheCallerIsAServiceOrAPerson(t *testing.T) {
	v := newVerifier(t)
	for _, c := range []struct {
		name  string
		sub   string
		roles []string
		ok    bool
	}{
		{"person with a UUID", testSub, []string{"SALESPERSON"}, true},
		{"person with a service name", "synkro-workflow", []string{"ADMIN"}, false},
		{"service with its name", "synkro-worker", []string{"SERVICE"}, true},
		{"service with a UUID", testSub, []string{"SERVICE"}, false},
		{"service with an upper-case name", "SYNKRO-WORKER", []string{"SERVICE"}, false},
		{"service with spaces", "synkro worker", []string{"SERVICE"}, false},
		{"service mixed with a person's role", "synkro-worker", []string{"SERVICE", "ADMIN"}, false},
		{"no roles but a UUID", testSub, []string{}, true},
	} {
		tok := rs256Token(validClaims(c.sub, c.roles, []string{}))
		if _, err := v.Verify(tok); (err == nil) != c.ok {
			t.Errorf("%s: accepted=%v, want %v", c.name, err == nil, c.ok)
		}
	}
}

func TestVerify_RejectsNullClaimsAndMalformedEncodings(t *testing.T) {
	v := newVerifier(t)
	base := validClaims(testSub, []string{"ADMIN"}, []string{})
	valid := rs256Token(base)
	for name, tok := range map[string]string{
		"roles null":       rs256Token(base.with("roles", nil)),
		"permissions null": rs256Token(base.with("permissions", nil)),
		"jti empty":        rs256Token(base.with("jti", "")),
		"permissions text": rs256Token(base.with("permissions", "products:read")),
		"empty token":      "",
		"padded token":     valid + "=",
		"leading space":    " " + valid,
	} {
		if _, err := v.Verify(tok); err == nil {
			t.Errorf("%s: want a rejection", name)
		}
	}
	if _, err := v.Verify(valid); err != nil {
		t.Errorf("control token rejected: %v", err)
	}
}

func TestVerify_ReturnsOnlyTheSentinelNeverAReason(t *testing.T) {
	v := newVerifier(t)
	for _, tok := range []string{"x", unsignedToken(validClaims(testSub, nil, nil), ""), rs256Token(validClaims(testSub, nil, nil).without("exp"))} {
		if _, err := v.Verify(tok); err != errInvalidToken {
			t.Errorf("want errInvalidToken, got %v", err)
		}
	}
}

func TestNewRS256Verifier_AcceptsPublicKeysInEveryConfiguredForm(t *testing.T) {
	pkix := publicKeyPEM(testKey)
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&testKey.PublicKey)}))
	for name, value := range map[string]string{
		"PKIX":                          pkix,
		"PKCS#1":                        pkcs1,
		"single line with literal \\n":  strings.ReplaceAll(strings.TrimSpace(pkix), "\n", `\n`),
		"with a trailing newline/space": pkix + "\n  ",
	} {
		v, err := NewRS256Verifier(value, fixedClock)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if _, err := v.Verify(rs256Token(claimsAt(time.Hour, testSub, []string{"ADMIN"}, []string{}))); err != nil {
			t.Errorf("%s: token not verified: %v", name, err)
		}
	}
}

func TestNewRS256Verifier_RejectsAnythingButAnRSAPublicKey(t *testing.T) {
	priv := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey)}))
	for name, value := range map[string]string{
		"empty":                "",
		"garbage":              "not a pem",
		"private key":          priv,
		"two blocks":           publicKeyPEM(testKey) + publicKeyPEM(otherKey),
		"trailing text":        publicKeyPEM(testKey) + "junk",
		"key under 2048 bits":  smallKeyPEM(),
		"certificate-typed":    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")})),
		"PUBLIC KEY not a key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("x")})),
	} {
		_, err := NewRS256Verifier(value, fixedClock)
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: error echoes key material: %v", name, err)
		}
	}
	if _, err := NewRS256Verifier(publicKeyPEM(testKey), nil); err == nil {
		t.Error("a nil clock must be rejected")
	}
}

func smallKeyPEM() string {
	k, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	return publicKeyPEM(k)
}

// countingVerifier lets the middleware tests prove the token is verified
// once per request however many authorization checks follow.
type countingVerifier struct {
	inner TokenVerifier
	calls atomic.Int32
}

func (c *countingVerifier) Verify(token string) (tokenClaims, error) {
	c.calls.Add(1)
	return c.inner.Verify(token)
}

func TestRequireAuth_VerifiesOncePerRequestAndHandsDownTheVerifiedClaims(t *testing.T) {
	cv := &countingVerifier{inner: newVerifier(t)}
	var seen tokenClaims
	var subject string
	var subjectOK bool
	h := requireAuth(cv, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = claimsFrom(r)
		_ = canReadCatalog(claimsFrom(r))
		_ = canWriteCatalog(claimsFrom(r))
		subject, subjectOK = subjectFrom(r)
	}))
	req := httptest.NewRequest("GET", "/api/v1/products", nil)
	req.Header.Set("Authorization", bearer(rs256Token(claimsAt(time.Hour, testSub, []string{"ADMIN"}, []string{"products:read"}))))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := cv.calls.Load(); got != 1 {
		t.Errorf("token verified %d times, want 1", got)
	}
	if seen.Sub != testSub || !subjectOK || subject != testSub {
		t.Errorf("handler saw %+v / %q,%v", seen, subject, subjectOK)
	}
}

func TestRequireAuth_NeverReachesTheHandlerWithoutABearerToken(t *testing.T) {
	cv := &countingVerifier{inner: newVerifier(t)}
	called := false
	h := requireAuth(cv, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, authorization := range []string{"", "Basic abc", "Bearer"} {
		req := httptest.NewRequest("GET", "/api/v1/products", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("%q: status %d, WWW-Authenticate %q", authorization, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	if called {
		t.Error("handler reached without a verified token")
	}
	if got := cv.calls.Load(); got != 0 {
		t.Errorf("verifier called %d times for requests with no Bearer token", got)
	}
}

func TestClaimsFrom_IsEmptyForARequestThatSkippedAuthentication(t *testing.T) {
	// Defence in depth: a handler wired outside requireAuth gets no access,
	// whatever the Authorization header says.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", bearer(rs256Token(validClaims(testSub, []string{"ADMIN"}, []string{"products:write"}))))
	if c := claimsFrom(req); c.Sub != "" || len(c.Roles) != 0 || len(c.Permissions) != 0 {
		t.Errorf("claims without verification: %+v", c)
	}
	if canWriteCatalog(claimsFrom(req)) {
		t.Error("an unverified request must not be authorized")
	}
	if _, ok := subjectFrom(req); ok {
		t.Error("an unverified request must have no subject")
	}
}
