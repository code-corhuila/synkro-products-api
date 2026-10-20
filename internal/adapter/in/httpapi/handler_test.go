package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHealth_ReturnsOkWithNoToken(t *testing.T) {
	srv := newTestAPI(t).srv

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("expected status ok, got %q", body.Status)
	}
	if body.Service != "synkro-products-api" {
		t.Errorf("expected service synkro-products-api, got %q", body.Service)
	}
}

func TestProtectedRoute_Returns401WithNoToken(t *testing.T) {
	srv := newTestAPI(t).srv

	resp, err := http.Get(srv.URL + "/api/v1/products")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var body struct {
		Error   string `json:"error"`
		TraceID string `json:"traceId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error != "UNAUTHORIZED" {
		t.Errorf("expected error UNAUTHORIZED, got %q", body.Error)
	}
	if body.TraceID == "" {
		t.Error("expected a non-empty traceId")
	}
}

func TestProtectedRoute_Returns401WithMalformedToken(t *testing.T) {
	srv := newTestAPI(t).srv

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/products", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 even with a malformed token, got %d", resp.StatusCode)
	}
}
