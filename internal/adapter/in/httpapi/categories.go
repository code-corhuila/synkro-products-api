package httpapi

import (
	"net/http"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

const categoryNameMax = 100

// categoryRequest is CreateCategoryRequest, which the contract reuses as
// the rename body.
type categoryRequest struct {
	Name *string `json:"name"`
}

type categoryResponse struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
}

func toCategoryResponse(c model.Category) categoryResponse {
	return categoryResponse{c.ID, c.Name, c.Active}
}

type categoryHandlers struct{ uc in.CategoryUseCases }

func (categoryHandlers) read(r *http.Request) (string, []errorDetail) {
	var req categoryRequest
	if details := decodeBody(r, &req); details != nil {
		return "", details
	}
	if d := checkName(req.Name, categoryNameMax); d != nil {
		return "", []errorDetail{*d}
	}
	return *req.Name, nil
}

func (h categoryHandlers) create(w http.ResponseWriter, r *http.Request) {
	key, keyDetails := requireIdempotencyKey(r)
	name, details := h.read(r)
	if details = append(keyDetails, details...); len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}

	res, err := h.uc.CreateCategory(r.Context(), in.CreateCategoryCommand{IdempotencyKey: key, Name: name})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	if !res.Created {
		writeJSON(w, http.StatusOK, toCategoryResponse(res.Category))
		return
	}
	w.Header().Set("Location", "/api/v1/products/categories/"+res.Category.ID)
	writeJSON(w, http.StatusCreated, toCategoryResponse(res.Category))
}

func (h categoryHandlers) list(w http.ResponseWriter, r *http.Request) {
	q := newQuery(r, "active")
	page, limit := q.page()
	active := q.optBool("active")
	if len(q.details) > 0 {
		writeValidationError(w, r, q.details)
		return
	}

	res, err := h.uc.ListCategories(r.Context(), in.ListCategoriesQuery{Page: page, Limit: limit, Active: active})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	data := make([]categoryResponse, 0, len(res.Items))
	for _, c := range res.Items {
		data = append(data, toCategoryResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": newPageMeta(page, limit, res.Total)})
}

func (h categoryHandlers) rename(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrCategoryNotFound)
		return
	}
	name, details := h.read(r)
	if len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}
	res, err := h.uc.RenameCategory(r.Context(), id, in.RenameCategoryCommand{Name: name})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCategoryResponse(res.Category))
}

// deactivate is the DELETE route: a soft delete that answers 200 with the
// resource, never 204.
func (h categoryHandlers) deactivate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrCategoryNotFound)
		return
	}
	res, err := h.uc.DeactivateCategory(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCategoryResponse(res.Category))
}
