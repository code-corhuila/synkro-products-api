package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type traceIDKey struct{}

// withCorrelation reuses the caller's X-Correlation-Id or generates one,
// returns it on every response and makes it available as the traceId of
// any error envelope (cross-cutting.md).
func withCorrelation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get("X-Correlation-Id")
		if traceID == "" {
			traceID = uuid.NewString()
		}
		w.Header().Set("X-Correlation-Id", traceID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceIDKey{}, traceID)))
	})
}

func traceIDFrom(r *http.Request) string {
	id, _ := r.Context().Value(traceIDKey{}).(string)
	return id
}

// requireAuth is a deliberately minimal default-deny gate: it rejects
// every request that lacks a well-formed Authorization header. It does
// NOT validate a real RS256 signature: any JWT-shaped Bearer token gets
// through. Its only job is to make sure nothing is reachable by
// accident.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !looksLikeJWT(token) {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "a valid Authorization header is required")
			return
		}
		// A real token still gets no further validation in this story —
		// any JWT-shaped Bearer token passes through. Real RS256
		// verification is explicitly out of scope here.
		next.ServeHTTP(w, r)
	})
}

// looksLikeJWT only checks the compact-serialization shape
// (header.payload.signature, all non-empty). It decodes nothing.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// subjectFrom reads the sub claim of the Bearer token as a UUID. Like
// requireAuth it verifies nothing: the signature is still not checked
// (real RS256 verification is deferred), so until then adjustedBy is only
// as trustworthy as the caller. It reports false for a token without a
// UUID sub, which cannot be stored in stock_adjustment.adjusted_by.
func subjectFrom(r *http.Request) (string, bool) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", false
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", false
	}
	return canonicalUUID(claims.Sub)
}
