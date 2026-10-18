package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

// NewRouter wires every route of this service. /health is public
// (cross-cutting.md §4); every other route is wrapped by the
// default-deny middleware added in step 3.
func NewRouter() http.Handler {
	// placeholder route so the 401 test has something to hit; real
	// product routes arrive with HU-PRO-01
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/products", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	root := http.NewServeMux()
	root.HandleFunc("GET /health", handleHealth)
	root.Handle("/api/v1/", requireAuth(protected))
	return root
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{
		Status:    "ok",
		Service:   "synkro-products-api",
		Timestamp: time.Now().UTC(),
	})
}
