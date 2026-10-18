package httpapi

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// clockSkew is the tolerance for exp, nbf and iat (ADR-006 Decision 1).
	clockSkew = 30 * time.Second

	// maxTokenLength bounds the work done for an unauthenticated caller.
	// A token of this service is about 1 KiB.
	maxTokenLength = 8192

	minRSAKeyBits = 2048
	roleService   = "SERVICE"
)

// errInvalidToken is the only failure a caller of Verify sees. The reason
// is deliberately not part of it: neither the response nor the logs may
// tell an attacker which check a forged token failed.
var errInvalidToken = errors.New("invalid authentication token")

// serviceName is the sub of a service token ("synkro-workflow",
// "synkro-worker"): a lower-case DNS-like label.
var serviceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// TokenVerifier turns a raw Bearer token into the claims this service
// authorizes on, or errInvalidToken. It is the seam between the HTTP
// middleware and the cryptography, so the middleware can be tested with
// the real verifier and the verifier alone with a fixed clock.
type TokenVerifier interface {
	Verify(token string) (tokenClaims, error)
}

// RS256Verifier verifies tokens signed by synkro-auth-api with RS256
// against its public key (authentication.md, "Validation rules"). The
// accepted algorithm is fixed here, never read from the token header.
type RS256Verifier struct {
	key    *rsa.PublicKey
	parser *jwt.Parser
}

// NewRS256Verifier parses the PEM public key (JWT_PUBLIC_KEY or the
// content of JWT_PUBLIC_KEY_FILE; literal "\n" sequences are accepted so
// the key fits one environment variable). now is the clock used for exp,
// nbf and iat.
func NewRS256Verifier(publicKeyPEM string, now func() time.Time) (*RS256Verifier, error) {
	key, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return nil, err
	}
	if now == nil {
		return nil, errors.New("a clock is required")
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
		jwt.WithTimeFunc(now),
	)
	return &RS256Verifier{key: key, parser: parser}, nil
}

func parseRSAPublicKey(value string) (*rsa.PublicKey, error) {
	block, rest := pem.Decode([]byte(strings.ReplaceAll(value, `\n`, "\n")))
	if block == nil || strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("JWT public key is not a single PEM block")
	}
	var key *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
			key, _ = parsed.(*rsa.PublicKey)
		}
	case "RSA PUBLIC KEY":
		key, _ = x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		// Notably a private key: only the public half may be configured.
		return nil, errors.New("JWT public key must be a PUBLIC KEY or RSA PUBLIC KEY PEM block")
	}
	if key == nil {
		return nil, errors.New("JWT public key is not a valid RSA public key")
	}
	if key.N.BitLen() < minRSAKeyBits {
		return nil, errors.New("JWT RSA public key is shorter than 2048 bits")
	}
	return key, nil
}

// wireClaims is the payload as issued by synkro-auth-api (authentication.md,
// "Token shape"). Slices stay nil when the claim is absent or null, which
// is how a missing claim is told from an empty one.
type wireClaims struct {
	jwt.RegisteredClaims
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// Verify checks, in this order, the size, the RS256 signature with the
// configured key, exp (required) and nbf/iat with a 30 s skew, and then the
// claims the contract requires: sub, roles, permissions, iat and jti. Any
// failure is errInvalidToken.
func (v *RS256Verifier) Verify(token string) (tokenClaims, error) {
	if token == "" || len(token) > maxTokenLength {
		return tokenClaims{}, errInvalidToken
	}
	var claims wireClaims
	parsed, err := v.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return v.key, nil })
	if err != nil || !parsed.Valid {
		return tokenClaims{}, errInvalidToken
	}
	if !numericTimeClaims(parsed) {
		return tokenClaims{}, errInvalidToken
	}
	if claims.IssuedAt == nil || claims.ID == "" || claims.Roles == nil || claims.Permissions == nil {
		return tokenClaims{}, errInvalidToken
	}
	if !validSubject(claims.Subject, claims.Roles) {
		return tokenClaims{}, errInvalidToken
	}
	return tokenClaims{Sub: claims.Subject, Roles: claims.Roles, Permissions: claims.Permissions}, nil
}

// validSubject: a person's sub is the user id, a canonical UUID; the sub
// of a service token (role SERVICE, which is token-only and never mixed
// with a person's role) is the service name.
func validSubject(sub string, roles []string) bool {
	if slices.Contains(roles, roleService) {
		return len(roles) == 1 && serviceName.MatchString(sub)
	}
	_, ok := canonicalUUID(sub)
	return ok
}

// numericTimeClaims rejects exp, nbf or iat sent as JSON strings. The JWT
// library decodes through json.Number, which would accept "exp":"9999999999";
// RFC 7519 §2 defines these claims as JSON numbers. The signature is
// already verified when this runs.
func numericTimeClaims(token *jwt.Token) bool {
	parts := strings.Split(token.Raw, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return false
	}
	for _, name := range []string{"exp", "nbf", "iat"} {
		if v, present := raw[name]; present && (len(v) == 0 || v[0] == '"') {
			return false
		}
	}
	return true
}
