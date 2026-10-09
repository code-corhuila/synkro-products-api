package httpapi

import (
	"net/http"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

const productNameMax = 150

// productRequest is both CreateProductRequest and UpdateProductRequest:
// the two schemas are identical. Pointers tell "missing" from "zero";
// any other field (stock included) is ignored.
type productRequest struct {
	Name       *string `json:"name"`
	PriceCents *int64  `json:"priceCents"`
	CategoryID *string `json:"categoryId"`
}

type productResponse struct {
	ProductID  string `json:"productId"`
	Name       string `json:"name"`
	PriceCents int64  `json:"priceCents"`
	Stock      int    `json:"stock"`
	CategoryID string `json:"categoryId"`
	Active     bool   `json:"active"`
}

func toProductResponse(p model.Product) productResponse {
	return productResponse{p.ID, p.Name, p.PriceCents, p.Stock, p.CategoryID, p.Active}
}

type productHandlers struct{ uc in.ProductUseCases }

// read decodes and validates a product body, one detail per bad field.
func (productHandlers) read(r *http.Request) (name string, priceCents int64, categoryID string, details []errorDetail) {
	var req productRequest
	if details := decodeBody(r, &req); details != nil {
		return "", 0, "", details
	}
	if d := checkName(req.Name, productNameMax); d != nil {
		details = append(details, *d)
	}
	if req.PriceCents == nil || *req.PriceCents < 1 {
		details = append(details, errorDetail{Field: "priceCents", Message: "required, an integer of 1 or more"})
	}
	if req.CategoryID == nil {
		details = append(details, errorDetail{Field: "categoryId", Message: "required"})
	} else if id, ok := canonicalUUID(*req.CategoryID); !ok {
		details = append(details, errorDetail{Field: "categoryId", Message: "must be a UUID"})
	} else {
		categoryID = id
	}
	if details != nil {
		return "", 0, "", details
	}
	return *req.Name, *req.PriceCents, categoryID, nil
}

func (h productHandlers) create(w http.ResponseWriter, r *http.Request) {
	key, keyDetails := requireIdempotencyKey(r)
	name, price, categoryID, details := h.read(r)
	if details = append(keyDetails, details...); len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}

	res, err := h.uc.CreateProduct(r.Context(), in.CreateProductCommand{
		IdempotencyKey: key, Name: name, PriceCents: price, CategoryID: categoryID,
	})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	if !res.Created {
		writeJSON(w, http.StatusOK, toProductResponse(res.Product))
		return
	}
	w.Header().Set("Location", "/api/v1/products/"+res.Product.ID)
	writeJSON(w, http.StatusCreated, toProductResponse(res.Product))
}

func (h productHandlers) list(w http.ResponseWriter, r *http.Request) {
	q := newQuery(r, "categoryId", "active", "name", "stockAtMost")
	page, limit := q.page()
	query := in.ListProductsQuery{
		Page:        page,
		Limit:       limit,
		CategoryID:  q.optUUID("categoryId"),
		Active:      q.optBool("active"),
		Name:        q.optString("name"),
		StockAtMost: q.optNonNegativeInt("stockAtMost"),
	}
	if len(q.details) > 0 {
		writeValidationError(w, r, q.details)
		return
	}
	// Same rule as getProduct: inactive products exist only for callers who
	// can write the catalog, so for anyone else the filter is forced to
	// active=true whatever was asked.
	if !canWriteCatalog(claimsFrom(r)) {
		onlyActive := true
		query.Active = &onlyActive
	}

	res, err := h.uc.ListProducts(r.Context(), query)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	data := make([]productResponse, 0, len(res.Items))
	for _, p := range res.Items {
		data = append(data, toProductResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": newPageMeta(page, limit, res.Total)})
}

func (h productHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	res, err := h.uc.GetProduct(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	// An inactive product exists only for callers who can write the catalog;
	// for anyone else it is indistinguishable from a missing one (contract:
	// "Returns the product if it exists and is active (or if the caller is
	// ADMIN/INVENTORY, including inactive)").
	if !res.Product.Active && !canWriteCatalog(claimsFrom(r)) {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toProductResponse(res.Product))
}

func (h productHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	name, price, categoryID, details := h.read(r)
	if len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}
	res, err := h.uc.UpdateProduct(r.Context(), id, in.UpdateProductCommand{Name: name, PriceCents: price, CategoryID: categoryID})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProductResponse(res.Product))
}

// deactivate is the DELETE route: a soft delete that answers 200 with the
// resource, never 204.
func (h productHandlers) deactivate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	res, err := h.uc.DeactivateProduct(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toProductResponse(res.Product))
}
