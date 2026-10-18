package httpapi

import (
	"context"
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

type claimsKey struct{}

// requireAuth is the authentication gate of every /api/v1 route: it
// verifies the Bearer token once (RS256 signature, exp, sub; see
// RS256Verifier) and stores the verified claims in the request context.
// Everything downstream (subjectFrom, claimsFrom, RBAC) reads those
// claims and never touches the Authorization header again. Whatever is
// wrong with the credential, the answer is the same 401.
func requireAuth(verifier TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			unauthorized(w, r)
			return
		}
		claims, err := verifier.Verify(token)
		if err != nil {
			unauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
	})
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid authentication token")
}

// tokenClaims are the verified claims of the Bearer token this service
// authorizes on (authentication.md, "Token shape").
type tokenClaims struct {
	Sub         string
	Roles       []string
	Permissions []string
}

// claimsFrom returns the claims requireAuth verified for this request. A
// request that did not pass through requireAuth has none, i.e. no access.
func claimsFrom(r *http.Request) tokenClaims {
	claims, _ := r.Context().Value(claimsKey{}).(tokenClaims)
	return claims
}

// subjectFrom reads the verified sub as a UUID. It reports false for a
// subject that is not one (a service token's sub is a service name), which
// cannot be stored in stock_adjustment.adjusted_by.
func subjectFrom(r *http.Request) (string, bool) {
	return canonicalUUID(claimsFrom(r).Sub)
}
