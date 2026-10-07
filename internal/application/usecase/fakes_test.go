package usecase

import (
	"context"
	"fmt"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Hand-written fakes for the use-case tier: maps plus call counters, so a
// test can prove a write did (or did not) happen. They mirror only the
// port contract, nothing about SQL.

type fakeProducts struct {
	byID    map[string]model.Product
	keys    map[string]string
	updates int
	listed  out.ProductFilter
	err     error // returned by every method when set
}

func newFakeProducts(ps ...model.Product) *fakeProducts {
	f := &fakeProducts{byID: map[string]model.Product{}, keys: map[string]string{}}
	for _, p := range ps {
		f.byID[p.ID] = p
	}
	return f
}

func (f *fakeProducts) CreateOnce(_ context.Context, key string, p model.Product) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	if id, ok := f.keys[key]; ok {
		return id, false, nil
	}
	f.keys[key] = p.ID
	f.byID[p.ID] = p
	return p.ID, true, nil
}

func (f *fakeProducts) FindByID(_ context.Context, id string) (model.Product, error) {
	if f.err != nil {
		return model.Product{}, f.err
	}
	p, ok := f.byID[id]
	if !ok {
		return model.Product{}, out.ErrNotFound
	}
	return p, nil
}

func (f *fakeProducts) List(_ context.Context, flt out.ProductFilter) ([]model.Product, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	f.listed = flt
	var items []model.Product
	for _, p := range f.byID {
		items = append(items, p)
	}
	return items, len(items), nil
}

func (f *fakeProducts) Update(_ context.Context, p model.Product) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.byID[p.ID]; !ok {
		return out.ErrNotFound
	}
	f.updates++
	f.byID[p.ID] = p
	return nil
}

type fakeCategories struct {
	byID         map[string]model.Category
	keys         map[string]string
	updates      int
	withProducts map[string]bool // categoryID -> has active products
	listedActive *bool
	listedPage   int
	listedLimit  int
	err          error
}

func newFakeCategories(cs ...model.Category) *fakeCategories {
	f := &fakeCategories{byID: map[string]model.Category{}, keys: map[string]string{}, withProducts: map[string]bool{}}
	for _, c := range cs {
		f.byID[c.ID] = c
	}
	return f
}

func (f *fakeCategories) activeNamed(name, exceptID string) bool {
	for _, c := range f.byID {
		if c.Active && c.Name == name && c.ID != exceptID {
			return true
		}
	}
	return false
}

func (f *fakeCategories) CreateOnce(_ context.Context, key string, c model.Category) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	if id, ok := f.keys[key]; ok {
		return id, false, nil
	}
	if c.Active && f.activeNamed(c.Name, c.ID) {
		return "", false, out.ErrDuplicateActiveName
	}
	f.keys[key] = c.ID
	f.byID[c.ID] = c
	return c.ID, true, nil
}

func (f *fakeCategories) FindByID(_ context.Context, id string) (model.Category, error) {
	if f.err != nil {
		return model.Category{}, f.err
	}
	c, ok := f.byID[id]
	if !ok {
		return model.Category{}, out.ErrNotFound
	}
	return c, nil
}

func (f *fakeCategories) FindActiveByName(_ context.Context, name string) (model.Category, bool, error) {
	if f.err != nil {
		return model.Category{}, false, f.err
	}
	for _, c := range f.byID {
		if c.Active && c.Name == name {
			return c, true, nil
		}
	}
	return model.Category{}, false, nil
}

func (f *fakeCategories) List(_ context.Context, page, limit int, active *bool) ([]model.Category, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	f.listedPage, f.listedLimit, f.listedActive = page, limit, active
	var items []model.Category
	for _, c := range f.byID {
		items = append(items, c)
	}
	return items, len(items), nil
}

func (f *fakeCategories) Update(_ context.Context, c model.Category) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.byID[c.ID]; !ok {
		return out.ErrNotFound
	}
	if c.Active && f.activeNamed(c.Name, c.ID) {
		return out.ErrDuplicateActiveName
	}
	f.updates++
	f.byID[c.ID] = c
	return nil
}

func (f *fakeCategories) HasActiveProducts(_ context.Context, categoryID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.withProducts[categoryID], nil
}

// sequentialIDs hands out predictable ids so tests can assert on them.
func sequentialIDs(prefix string) func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("%s-%d", prefix, n)
	}
}
