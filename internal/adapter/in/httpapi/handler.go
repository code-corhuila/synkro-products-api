package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
)

type healthResponse struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

// NewRouter wires every route of this service. /health is public
// (cross-cutting.md §4); every other route is wrapped by the
// default-deny middleware.
func NewRouter(products in.ProductUseCases, categories in.CategoryUseCases, stock in.StockUseCases) http.Handler {
	p := productHandlers{products}
	c := categoryHandlers{categories}
	s := stockHandlers{stock}

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/products", p.list)
	protected.HandleFunc("POST /api/v1/products", p.create)
	protected.HandleFunc("GET /api/v1/products/{id}", p.get)
	protected.HandleFunc("PUT /api/v1/products/{id}", p.update)
	protected.HandleFunc("DELETE /api/v1/products/{id}", p.deactivate)

	// "categories" is a literal segment, so these patterns are more
	// specific than /products/{id} and win over it.
	protected.HandleFunc("GET /api/v1/products/categories", c.list)
	protected.HandleFunc("POST /api/v1/products/categories", c.create)
	protected.HandleFunc("PUT /api/v1/products/categories/{id}", c.rename)
	protected.HandleFunc("DELETE /api/v1/products/categories/{id}", c.deactivate)

	protected.HandleFunc("POST /api/v1/products/{id}/stock-adjustments", s.createAdjustment)

	// Internal, service-to-service (the workflow saga); not routed by the gateway.
	protected.HandleFunc("POST /api/v1/stock-reservations", s.createReservation)
	protected.HandleFunc("GET /api/v1/stock-reservations/{id}", s.getReservation)
	protected.HandleFunc("POST /api/v1/stock-reservations/{id}/release", s.releaseReservation)

	protected.HandleFunc("/", writeNotFound)

	root := http.NewServeMux()
	root.HandleFunc("GET /health", handleHealth)
	root.Handle("/api/v1/", requireAuth(protected))
	root.HandleFunc("/", writeNotFound)
	return withCorrelation(root)
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
