package persistence

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Memory is the in-memory store behind the HTTP-tier tests. It mirrors
// the behavior of products_schema that the use cases can observe: one
// idempotency-key space shared by every resource type, the
// active-only category name rule, newest-first lists, and an Update that
// never writes stock.
type Memory struct {
	mu         sync.Mutex
	seq        int
	products   map[string]memRow[model.Product]
	categories map[string]memRow[model.Category]
	keys       map[string]memKey
}

type memRow[T any] struct {
	v   T
	seq int // insertion order, standing in for the UUIDv7 ordering
}

type memKey struct {
	resourceType string
	id           string
}

const (
	resourceProduct  = "PRODUCT"
	resourceCategory = "CATEGORY"
)

func NewMemory() *Memory {
	return &Memory{
		products:   map[string]memRow[model.Product]{},
		categories: map[string]memRow[model.Category]{},
		keys:       map[string]memKey{},
	}
}

func (m *Memory) Products() out.ProductRepository    { return memoryProducts{m} }
func (m *Memory) Categories() out.CategoryRepository { return memoryCategories{m} }

// SeedProduct stores p as-is, stock included. Stock has no write path in
// this story (it arrives with HU-PRO-09), so tests that need a non-zero
// stock put it here.
func (m *Memory) SeedProduct(p model.Product) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	m.products[p.ID] = memRow[model.Product]{p, m.seq}
}

// claimKey must be called with mu held. It reports the id already stored
// under key, if any.
func (m *Memory) claimKey(key, resourceType string) (id string, replay bool, err error) {
	k, ok := m.keys[key]
	if !ok {
		return "", false, nil
	}
	if k.resourceType != resourceType {
		return "", false, out.ErrIdempotencyKeyReused
	}
	return k.id, true, nil
}

func (m *Memory) activeNameTaken(name, exceptID string) bool {
	for _, r := range m.categories {
		if r.v.Active && r.v.Name == name && r.v.ID != exceptID {
			return true
		}
	}
	return false
}

func page[T any](rows []memRow[T], pageNum, limit int) ([]T, int) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].seq > rows[j].seq })
	total := len(rows)
	start := (pageNum - 1) * limit
	items := []T{}
	for i := start; i < total && i < start+limit; i++ {
		items = append(items, rows[i].v)
	}
	return items, total
}

type memoryProducts struct{ m *Memory }

func (r memoryProducts) CreateOnce(_ context.Context, key string, p model.Product) (string, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if id, replay, err := r.m.claimKey(key, resourceProduct); err != nil || replay {
		return id, false, err
	}
	r.m.seq++
	r.m.products[p.ID] = memRow[model.Product]{p, r.m.seq}
	r.m.keys[key] = memKey{resourceProduct, p.ID}
	return p.ID, true, nil
}

func (r memoryProducts) FindByID(_ context.Context, id string) (model.Product, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.products[id]
	if !ok {
		return model.Product{}, out.ErrNotFound
	}
	return row.v, nil
}

func (r memoryProducts) List(_ context.Context, f out.ProductFilter) ([]model.Product, int, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	var rows []memRow[model.Product]
	for _, row := range r.m.products {
		p := row.v
		if f.CategoryID != nil && p.CategoryID != *f.CategoryID {
			continue
		}
		if f.Active != nil && p.Active != *f.Active {
			continue
		}
		if f.Name != nil && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(*f.Name)) {
			continue
		}
		if f.StockAtMost != nil && p.Stock > *f.StockAtMost {
			continue
		}
		rows = append(rows, row)
	}
	items, total := page(rows, f.Page, f.Limit)
	return items, total, nil
}

func (r memoryProducts) Update(_ context.Context, p model.Product) error {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.products[p.ID]
	if !ok {
		return out.ErrNotFound
	}
	p.Stock = row.v.Stock
	row.v = p
	r.m.products[p.ID] = row
	return nil
}

type memoryCategories struct{ m *Memory }

func (r memoryCategories) CreateOnce(_ context.Context, key string, c model.Category) (string, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if id, replay, err := r.m.claimKey(key, resourceCategory); err != nil || replay {
		return id, false, err
	}
	if c.Active && r.m.activeNameTaken(c.Name, c.ID) {
		return "", false, out.ErrDuplicateActiveName
	}
	r.m.seq++
	r.m.categories[c.ID] = memRow[model.Category]{c, r.m.seq}
	r.m.keys[key] = memKey{resourceCategory, c.ID}
	return c.ID, true, nil
}

func (r memoryCategories) FindByID(_ context.Context, id string) (model.Category, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.categories[id]
	if !ok {
		return model.Category{}, out.ErrNotFound
	}
	return row.v, nil
}

func (r memoryCategories) FindActiveByName(_ context.Context, name string) (model.Category, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	for _, row := range r.m.categories {
		if row.v.Active && row.v.Name == name {
			return row.v, true, nil
		}
	}
	return model.Category{}, false, nil
}

func (r memoryCategories) List(_ context.Context, pageNum, limit int, active *bool) ([]model.Category, int, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	var rows []memRow[model.Category]
	for _, row := range r.m.categories {
		if active != nil && row.v.Active != *active {
			continue
		}
		rows = append(rows, row)
	}
	items, total := page(rows, pageNum, limit)
	return items, total, nil
}

func (r memoryCategories) Update(_ context.Context, c model.Category) error {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.categories[c.ID]
	if !ok {
		return out.ErrNotFound
	}
	if c.Active && r.m.activeNameTaken(c.Name, c.ID) {
		return out.ErrDuplicateActiveName
	}
	row.v = c
	r.m.categories[c.ID] = row
	return nil
}

func (r memoryCategories) HasActiveProducts(_ context.Context, categoryID string) (bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	for _, row := range r.m.products {
		if row.v.CategoryID == categoryID && row.v.Active {
			return true, nil
		}
	}
	return false, nil
}
