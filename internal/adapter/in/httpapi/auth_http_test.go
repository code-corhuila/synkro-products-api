package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Every authenticated route of the service, with method and a concrete
// path. A 401 must be answered before any of them reaches a handler.
var allProtectedRoutes = []struct{ method, path string }{
	{"GET", "/api/v1/products"},
	{"POST", "/api/v1/products"},
	{"GET", "/api/v1/products/0192a000-0000-7000-8000-0000000000b1"},
	{"PUT", "/api/v1/products/0192a000-0000-7000-8000-0000000000b1"},
	{"DELETE", "/api/v1/products/0192a000-0000-7000-8000-0000000000b1"},
	{"GET", "/api/v1/products/categories"},
	{"POST", "/api/v1/products/categories"},
	{"PUT", "/api/v1/products/categories/0192a000-0000-7000-8000-0000000000c1"},
	{"DELETE", "/api/v1/products/categories/0192a000-0000-7000-8000-0000000000c1"},
	{"POST", "/api/v1/products/0192a000-0000-7000-8000-0000000000b1/stock-adjustments"},
	{"POST", "/api/v1/stock-reservations"},
	{"GET", "/api/v1/stock-reservations/0192a000-0000-7000-8000-0000000000d1"},
	{"POST", "/api/v1/stock-reservations/0192a000-0000-7000-8000-0000000000d1/release"},
	{"GET", "/api/v1/stock-alerts"},
	{"POST", "/api/v1/stock-alerts"},
	{"POST", "/api/v1/stock-alerts/0192a000-0000-7000-8000-0000000000e1/resolve"},
}

// send issues a request with the given raw Authorization header ("" means
// the header is absent) and extra headers.
func (a *testAPI) send(method, path, authorization string, headers ...string) (apiResponse, error) {
	req, err := http.NewRequest(method, a.srv.URL+path, strings.NewReader("{}"))
	if err != nil {
		return apiResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "auth-test-key")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return apiResponse{status: resp.StatusCode, header: resp.Header, body: raw}, nil
}

func bearer(token string) string { return "Bearer " + token }

func (a *testAPI) mustSend(method, path, authorization string, headers ...string) apiResponse {
	a.t.Helper()
	r, err := a.send(method, path, authorization, headers...)
	if err != nil {
		a.t.Fatal(err)
	}
	return r
}

// Authentication rejections, each tried against every protected route.

func TestAuth_RejectsCredentialsThatAreNotAValidRS256Token(t *testing.T) {
	adminClaims := validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})
	valid := rs256Token(adminClaims)
	now := time.Now()

	cases := []struct{ name, authorization string }{
		{"no Authorization header", ""},
		{"Basic scheme", "Basic " + valid},
		{"Token scheme", "Token " + valid},
		{"no scheme", valid},
		{"Bearer with no token", "Bearer"},
		{"Bearer with an empty token", "Bearer "},
		{"Bearer with a double space", "Bearer  " + valid},
		{"token with two segments", bearer(strings.Join(strings.Split(valid, ".")[:2], "."))},
		{"token with four segments", bearer(valid + ".AAAA")},
		{"opaque string", bearer("not-a-real-token")},
		{"oversized token", bearer(strings.Repeat("A", 20000) + "." + strings.Repeat("B", 20000) + ".C")},

		{"tampered payload keeping the signature", bearer(tamperedPayload(valid, validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write", "stock:reserve", "stock-alerts:write"})))},
		{"tampered signature", bearer(flippedSignature(valid))},
		{"signed with another RSA key", bearer(signedWith(otherKey, map[string]any{"alg": "RS256", "typ": "JWT"}, adminClaims))},

		{"alg none with empty signature", bearer(unsignedToken(adminClaims, ""))},
		{"alg none with a garbage signature", bearer(unsignedToken(adminClaims, "c2ln"))},
		{"HS256 keyed with the public key", bearer(hs256Token(publicKeyPEM(testKey), adminClaims))},
		{"RS384 header", bearer(signedWith(testKey, map[string]any{"alg": "RS384", "typ": "JWT"}, adminClaims))},
		{"header without alg", bearer(signedWith(testKey, map[string]any{"typ": "JWT"}, adminClaims))},

		{"expired well beyond the skew", bearer(rs256Token(adminClaims.with("exp", now.Add(-time.Hour).Unix())))},
		{"expired just beyond the 30 s skew", bearer(rs256Token(adminClaims.with("exp", now.Add(-time.Minute).Unix())))},
		{"exp missing", bearer(rs256Token(adminClaims.without("exp")))},
		{"exp as a string", bearer(rs256Token(adminClaims.with("exp", "9999999999")))},
		{"sub missing", bearer(rs256Token(adminClaims.without("sub")))},
		{"sub empty", bearer(rs256Token(adminClaims.with("sub", "")))},
		{"sub not a UUID for a person", bearer(rs256Token(adminClaims.with("sub", "admin")))},
		{"sub not a string", bearer(rs256Token(adminClaims.with("sub", 42)))},
		{"iat missing", bearer(rs256Token(adminClaims.without("iat")))},
		{"jti missing", bearer(rs256Token(adminClaims.without("jti")))},
		{"roles missing", bearer(rs256Token(adminClaims.without("roles")))},
		{"permissions missing", bearer(rs256Token(adminClaims.without("permissions")))},
		{"roles not an array", bearer(rs256Token(adminClaims.with("roles", "ADMIN")))},
		{"issued far in the future", bearer(rs256Token(adminClaims.with("iat", now.Add(time.Hour).Unix())))},
		{"not valid yet (nbf in the future)", bearer(rs256Token(adminClaims.with("nbf", now.Add(time.Hour).Unix())))},
	}

	a := newTestAPI(t)
	for _, c := range cases {
		for _, route := range allProtectedRoutes {
			r := a.mustSend(route.method, route.path, c.authorization)
			if r.status != http.StatusUnauthorized {
				t.Errorf("%s: %s %s answered %d, want 401", c.name, route.method, route.path, r.status)
			}
		}
	}
}

func TestAuth_UnauthorizedAnswersTheContractEnvelopeWithoutSayingWhy(t *testing.T) {
	a := newTestAPI(t)
	adminClaims := validClaims(testSub, []string{"ADMIN"}, []string{"products:read"})

	reasons := map[string]string{
		"absent":        "",
		"bad signature": bearer(signedWith(otherKey, map[string]any{"alg": "RS256"}, adminClaims)),
		"expired":       bearer(rs256Token(adminClaims.with("exp", time.Now().Add(-time.Hour).Unix()))),
		"alg none":      bearer(unsignedToken(adminClaims, "")),
	}
	messages := map[string]bool{}
	for name, authorization := range reasons {
		r := a.mustSend("GET", "/api/v1/products", authorization, "X-Correlation-Id", "trace-"+strings.ReplaceAll(name, " ", "-"))
		if r.status != http.StatusUnauthorized {
			t.Fatalf("%s: want 401, got %d", name, r.status)
		}
		e := r.errorBody(t)
		if e.Error != "UNAUTHORIZED" {
			t.Errorf("%s: error code %q, want UNAUTHORIZED", name, e.Error)
		}
		if e.TraceID != "trace-"+strings.ReplaceAll(name, " ", "-") || r.header.Get("X-Correlation-Id") != e.TraceID {
			t.Errorf("%s: traceId %q / header %q do not carry the caller's correlation id", name, e.TraceID, r.header.Get("X-Correlation-Id"))
		}
		messages[e.Message] = true
		body := string(r.body)
		for _, leak := range []string{"signature", "expired", "RS256", "alg", "key", "claim"} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
				t.Errorf("%s: 401 body reveals %q: %s", name, leak, body)
			}
		}
	}
	if len(messages) != 1 {
		t.Errorf("401 messages differ by failure reason (%d variants): %v", len(messages), messages)
	}
}

func TestAuth_HealthStaysPublicEvenWithAnInvalidToken(t *testing.T) {
	a := newTestAPI(t)
	for _, authorization := range []string{"", bearer("garbage"), bearer(unsignedToken(validClaims(testSub, nil, nil), ""))} {
		if r := a.mustSend("GET", "/health", authorization); r.status != http.StatusOK {
			t.Errorf("GET /health with %q answered %d, want 200", authorization, r.status)
		}
	}
}

func TestAuth_IgnoresIdentityHeaders(t *testing.T) {
	a := newTestAPI(t)
	salesperson := rs256Token(validClaims(testSub, []string{"SALESPERSON"}, []string{"products:read"}))

	r := a.mustSend("POST", "/api/v1/products", bearer(salesperson),
		"X-User-Id", testSub, "X-User-Roles", "ADMIN", "X-User-Permissions", "products:write")
	if r.status != http.StatusForbidden {
		t.Errorf("X-User-* headers must not widen a verified token: got %d, want 403", r.status)
	}
}

func TestAuth_ForgedClaimsNeverReachAuthorization(t *testing.T) {
	a := newTestAPI(t)
	forged := validClaims(testSub, []string{"SERVICE"}, []string{"stock:reserve", "stock:release", "stock-alerts:write", "products:write"}).with("sub", "synkro-workflow")

	for _, token := range []string{
		unsignedToken(forged, ""),
		signedWith(otherKey, map[string]any{"alg": "RS256"}, forged),
		hs256Token(publicKeyPEM(testKey), forged),
	} {
		for _, route := range []struct{ method, path string }{
			{"POST", "/api/v1/stock-reservations"},
			{"POST", "/api/v1/stock-alerts"},
			{"POST", "/api/v1/products"},
		} {
			if r := a.mustSend(route.method, route.path, bearer(token)); r.status != http.StatusUnauthorized {
				t.Errorf("forged SERVICE token on %s %s answered %d, want 401", route.method, route.path, r.status)
			}
		}
	}
}

// Accepted tokens and the clock-skew boundary.

func TestAuth_AcceptsAValidRS256TokenAndToleratesTheClockSkew(t *testing.T) {
	a := newTestAPI(t)
	base := validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})
	now := time.Now()

	cases := []struct {
		name  string
		token string
	}{
		{"fresh token", rs256Token(base)},
		{"expired 10 s ago (inside the 30 s skew)", rs256Token(base.with("exp", now.Add(-10*time.Second).Unix()))},
		{"issued 10 s in the future (inside the skew)", rs256Token(base.with("iat", now.Add(10*time.Second).Unix()))},
		{"nbf 10 s in the future (inside the skew)", rs256Token(base.with("nbf", now.Add(10*time.Second).Unix()))},
		{"extra unknown claims and header fields", signedWith(testKey, map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"}, base.with("tenant", "x"))},
	}
	for _, c := range cases {
		if r := a.mustSend("GET", "/api/v1/products", bearer(c.token)); r.status != http.StatusOK {
			t.Errorf("%s: answered %d, want 200: %s", c.name, r.status, r.body)
		}
	}
}

// Authorization still runs on top of authentication: a genuine token with
// too little role or permission is 403, never 401 and never 2xx.

func TestAuth_AValidTokenWithoutTheRoleOrPermissionIs403(t *testing.T) {
	a := newTestAPI(t)
	salesperson := rs256Token(validClaims(testSub, []string{"SALESPERSON"}, []string{"products:read"}))
	inventory := rs256Token(validClaims(testSub, []string{"INVENTORY"}, []string{"products:read", "products:write"}))
	workflow := rs256Token(validClaims("synkro-workflow", []string{"SERVICE"}, []string{"customers:read", "stock:reserve", "stock:release", "sales:register"}))
	worker := rs256Token(validClaims("synkro-worker", []string{"SERVICE"}, []string{"products:read", "stock-alerts:read", "stock-alerts:write"}))

	cases := []struct {
		name, token, method, path string
	}{
		{"SALESPERSON writes the catalog", salesperson, "POST", "/api/v1/products"},
		{"SALESPERSON adjusts stock", salesperson, "POST", "/api/v1/products/0192a000-0000-7000-8000-0000000000b1/stock-adjustments"},
		{"SALESPERSON reads alerts", salesperson, "GET", "/api/v1/stock-alerts"},
		{"SALESPERSON reserves stock", salesperson, "POST", "/api/v1/stock-reservations"},
		{"INVENTORY reserves stock", inventory, "POST", "/api/v1/stock-reservations"},
		{"INVENTORY opens an alert", inventory, "POST", "/api/v1/stock-alerts"},
		{"workflow writes the catalog", workflow, "POST", "/api/v1/products"},
		{"workflow opens an alert", workflow, "POST", "/api/v1/stock-alerts"},
		{"worker reserves stock", worker, "POST", "/api/v1/stock-reservations"},
		{"worker releases a reservation", worker, "POST", "/api/v1/stock-reservations/0192a000-0000-7000-8000-0000000000d1/release"},
		{"worker writes the catalog", worker, "POST", "/api/v1/products"},
	}
	for _, c := range cases {
		if r := a.mustSend(c.method, c.path, bearer(c.token)); r.status != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", c.name, r.status)
		}
	}
}

// Genuine human and SERVICE tokens pass authentication and authorization
// for the operations the contract gives them (neither 401 nor 403).

func TestAuth_GenuineHumanAndServiceTokensReachTheirOperations(t *testing.T) {
	a := newTestAPI(t)
	token := func(sub string, roles, perms []string) string { return rs256Token(validClaims(sub, roles, perms)) }
	admin := token(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})
	inventory := token(testSub, []string{"INVENTORY"}, []string{"products:read", "products:write"})
	salesperson := token(testSub, []string{"SALESPERSON"}, []string{"products:read"})
	workflow := token("synkro-workflow", []string{"SERVICE"}, []string{"stock:reserve", "stock:release"})
	worker := token("synkro-worker", []string{"SERVICE"}, []string{"products:read", "stock-alerts:read", "stock-alerts:write"})

	cases := []struct {
		name, token, method, path string
	}{
		{"ADMIN lists products", admin, "GET", "/api/v1/products"},
		{"ADMIN creates a category", admin, "POST", "/api/v1/products/categories"},
		{"ADMIN lists alerts", admin, "GET", "/api/v1/stock-alerts"},
		{"INVENTORY lists products", inventory, "GET", "/api/v1/products"},
		{"INVENTORY creates a category", inventory, "POST", "/api/v1/products/categories"},
		{"INVENTORY lists alerts", inventory, "GET", "/api/v1/stock-alerts"},
		{"SALESPERSON lists products", salesperson, "GET", "/api/v1/products"},
		{"SALESPERSON lists categories", salesperson, "GET", "/api/v1/products/categories"},
		{"workflow reserves stock", workflow, "POST", "/api/v1/stock-reservations"},
		{"workflow releases a reservation", workflow, "POST", "/api/v1/stock-reservations/0192a000-0000-7000-8000-0000000000d1/release"},
		{"worker lists products", worker, "GET", "/api/v1/products"},
		{"worker lists alerts", worker, "GET", "/api/v1/stock-alerts"},
		{"worker opens an alert", worker, "POST", "/api/v1/stock-alerts"},
		{"worker resolves an alert", worker, "POST", "/api/v1/stock-alerts/0192a000-0000-7000-8000-0000000000e1/resolve"},
	}
	for _, c := range cases {
		r := a.mustSend(c.method, c.path, bearer(c.token))
		if r.status == http.StatusUnauthorized || r.status == http.StatusForbidden {
			t.Errorf("%s: answered %d, want the operation to be reached", c.name, r.status)
		}
	}
}
