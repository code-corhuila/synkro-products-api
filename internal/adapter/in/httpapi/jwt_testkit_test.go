package httpapi

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"time"

	"github.com/google/uuid"
)

// Test-only RSA keys generated at start-up: nothing secret is committed.
// testKey stands in for synkro-auth-api's signing key (the service is
// configured with its public half); otherKey is an unrelated key pair.
var (
	testKey  = mustRSAKey()
	otherKey = mustRSAKey()
)

func mustRSAKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}

func publicKeyPEM(k *rsa.PrivateKey) string {
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

var b64 = base64.RawURLEncoding.EncodeToString

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// claimSet is a mutable JWT payload, so a test can drop or corrupt one
// claim of an otherwise valid token.
type claimSet map[string]any

// validClaims is the payload synkro-auth-api issues (authentication.md,
// "Token shape"), valid for one hour from now.
func validClaims(sub string, roles, permissions []string) claimSet {
	if roles == nil {
		roles = []string{}
	}
	if permissions == nil {
		permissions = []string{}
	}
	now := time.Now()
	return claimSet{
		"sub":         sub,
		"roles":       roles,
		"permissions": permissions,
		"iat":         now.Unix(),
		"exp":         now.Add(time.Hour).Unix(),
		"jti":         uuid.NewString(),
	}
}

func (c claimSet) without(name string) claimSet {
	out := claimSet{}
	for k, v := range c {
		if k != name {
			out[k] = v
		}
	}
	return out
}

func (c claimSet) with(name string, value any) claimSet {
	out := c.without(name)
	out[name] = value
	return out
}

// signedWith builds header.payload.signature, signing the first two
// segments with the given RS256 key.
func signedWith(key *rsa.PrivateKey, header map[string]any, claims claimSet) string {
	signingInput := b64(mustJSON(header)) + "." + b64(mustJSON(claims))
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signingInput + "." + b64(sig)
}

func rs256Token(claims claimSet) string {
	return signedWith(testKey, map[string]any{"alg": "RS256", "typ": "JWT"}, claims)
}

// hs256Token is the classic algorithm-confusion forgery: an HMAC keyed
// with the service's own public key PEM.
func hs256Token(secret string, claims claimSet) string {
	signingInput := b64(mustJSON(map[string]any{"alg": "HS256", "typ": "JWT"})) + "." + b64(mustJSON(claims))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64(mac.Sum(nil))
}

// unsignedToken is an `alg: none` token; sig may be empty or garbage.
func unsignedToken(claims claimSet, sig string) string {
	return b64(mustJSON(map[string]any{"alg": "none", "typ": "JWT"})) + "." + b64(mustJSON(claims)) + "." + sig
}

// tamperedPayload keeps header and signature of a valid token but swaps
// in a different payload.
func tamperedPayload(valid string, claims claimSet) string {
	parts := splitToken(valid)
	return parts[0] + "." + b64(mustJSON(claims)) + "." + parts[2]
}

func splitToken(token string) [3]string {
	var out [3]string
	i, j := -1, -1
	for n, r := range token {
		if r == '.' {
			if i < 0 {
				i = n
			} else {
				j = n
				break
			}
		}
	}
	out[0], out[1], out[2] = token[:i], token[i+1:j], token[j+1:]
	return out
}

// flippedSignature corrupts one byte in the middle of the signature.
func flippedSignature(valid string) string {
	parts := splitToken(valid)
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[len(sig)/2] ^= 0x01
	return parts[0] + "." + parts[1] + "." + b64(sig)
}
