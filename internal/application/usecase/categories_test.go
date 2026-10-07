package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

func newCategoryService(cs *fakeCategories) *CategoryService {
	return NewCategoryService(cs, sequentialIDs("cat"))
}

func TestCreateCategory_CreatesAnActiveCategory(t *testing.T) {
	cs := newFakeCategories()
	svc := newCategoryService(cs)

	res, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: "Peripherals"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := model.Category{ID: "cat-1", Name: "Peripherals", Active: true}
	if !res.Created || res.Category != want || cs.byID["cat-1"] != want {
		t.Errorf("got %+v (created=%v), stored %+v", res.Category, res.Created, cs.byID["cat-1"])
	}
}

// 422, not 404: a name collision is a business-rule violation.
func TestCreateCategory_DuplicateActiveNameIsABusinessRuleViolationNotNotFound(t *testing.T) {
	cs := newFakeCategories(activeCat)
	svc := newCategoryService(cs)

	_, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: activeCat.Name})
	if !errors.Is(err, in.ErrCategoryNameTaken) {
		t.Fatalf("expected ErrCategoryNameTaken, got %v", err)
	}
	if errors.Is(err, in.ErrCategoryNotFound) {
		t.Fatal("a name collision must not be reported as not found")
	}
	if len(cs.byID) != 1 {
		t.Error("nothing should have been stored")
	}
}

func TestCreateCategory_NameOfAnInactiveCategoryCanBeReused(t *testing.T) {
	svc := newCategoryService(newFakeCategories(inactiveCat))
	_, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: inactiveCat.Name})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestCreateCategory_NameUniquenessIsCaseSensitiveLikeTheDatabaseIndex(t *testing.T) {
	svc := newCategoryService(newFakeCategories(activeCat))
	_, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: "peripherals"})
	if err != nil {
		t.Fatalf("uq_category_name_active is case-sensitive, so this must succeed; got %v", err)
	}
}

// The replay must be recognised before the name rule runs: the original
// category now owns that name, so a "check the name first" order would
// answer 422 to a legitimate retry.
func TestCreateCategory_ReplayReturnsTheOriginalEvenThoughItsNameIsNowTaken(t *testing.T) {
	cs := newFakeCategories()
	svc := newCategoryService(cs)

	first, _ := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: "Peripherals"})
	again, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: "Peripherals"})
	if err != nil {
		t.Fatalf("a replay must succeed, got %v", err)
	}
	if again.Created || again.Category != first.Category {
		t.Errorf("replay: got %+v (created=%v), want the original %+v", again.Category, again.Created, first.Category)
	}
	if len(cs.byID) != 1 {
		t.Errorf("expected 1 category stored, got %d", len(cs.byID))
	}
}

func TestCreateCategory_EmptyNameIsRejected(t *testing.T) {
	svc := newCategoryService(newFakeCategories())
	_, err := svc.CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: ""})
	if !errors.Is(err, model.ErrCategoryNameRequired) {
		t.Fatalf("expected ErrCategoryNameRequired, got %v", err)
	}
}

func TestCreateCategory_KeyReusedForAnotherResourceIsReported(t *testing.T) {
	cs := newFakeCategories()
	cs.err = out.ErrIdempotencyKeyReused
	_, err := newCategoryService(cs).CreateCategory(context.Background(), in.CreateCategoryCommand{IdempotencyKey: "key-0001", Name: "X"})
	if !errors.Is(err, in.ErrIdempotencyKeyReused) {
		t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
	}
}

func TestListCategories_PassesPageLimitAndFilterThrough(t *testing.T) {
	cs := newFakeCategories(activeCat, inactiveCat)
	active := false
	res, err := newCategoryService(cs).ListCategories(context.Background(), in.ListCategoriesQuery{Page: 3, Limit: 7, Active: &active})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs.listedPage != 3 || cs.listedLimit != 7 || cs.listedActive == nil || *cs.listedActive {
		t.Errorf("not passed through: page=%d limit=%d active=%v", cs.listedPage, cs.listedLimit, cs.listedActive)
	}
	if res.Total != 2 {
		t.Errorf("expected total 2, got %d", res.Total)
	}
}

func TestRenameCategory_Renames(t *testing.T) {
	cs := newFakeCategories(activeCat)
	res, err := newCategoryService(cs).RenameCategory(context.Background(), activeCat.ID, in.RenameCategoryCommand{Name: "Accessories"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Category.Name != "Accessories" || cs.byID[activeCat.ID].Name != "Accessories" {
		t.Errorf("not renamed: %+v", res.Category)
	}
}

func TestRenameCategory_ToItsOwnCurrentNameIsNotADuplicate(t *testing.T) {
	cs := newFakeCategories(activeCat)
	res, err := newCategoryService(cs).RenameCategory(context.Background(), activeCat.ID, in.RenameCategoryCommand{Name: activeCat.Name})
	if err != nil {
		t.Fatalf("renaming a category to its own name must not reject itself, got %v", err)
	}
	if res.Category != activeCat {
		t.Errorf("got %+v", res.Category)
	}
}

// 422, not 404, on rename as well.
func TestRenameCategory_ToAnotherActiveCategorysNameIsABusinessRuleViolation(t *testing.T) {
	other := model.Category{ID: "cat-other", Name: "Audio", Active: true}
	cs := newFakeCategories(activeCat, other)
	_, err := newCategoryService(cs).RenameCategory(context.Background(), other.ID, in.RenameCategoryCommand{Name: activeCat.Name})
	if !errors.Is(err, in.ErrCategoryNameTaken) {
		t.Fatalf("expected ErrCategoryNameTaken, got %v", err)
	}
	if cs.updates != 0 || cs.byID[other.ID].Name != "Audio" {
		t.Error("the category must not change")
	}
}

// uq_category_name_active only covers active rows, so an inactive
// category may carry an active one's name; the application must agree.
func TestRenameCategory_AnInactiveCategoryMayTakeAnActiveName(t *testing.T) {
	cs := newFakeCategories(activeCat, inactiveCat)
	_, err := newCategoryService(cs).RenameCategory(context.Background(), inactiveCat.ID, in.RenameCategoryCommand{Name: activeCat.Name})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestRenameCategory_MissingIsNotFound(t *testing.T) {
	_, err := newCategoryService(newFakeCategories()).RenameCategory(context.Background(), "nope", in.RenameCategoryCommand{Name: "X"})
	if !errors.Is(err, in.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
}

func TestRenameCategory_EmptyNameIsRejected(t *testing.T) {
	_, err := newCategoryService(newFakeCategories(activeCat)).RenameCategory(context.Background(), activeCat.ID, in.RenameCategoryCommand{Name: ""})
	if !errors.Is(err, model.ErrCategoryNameRequired) {
		t.Fatalf("expected ErrCategoryNameRequired, got %v", err)
	}
}

// A concurrent writer can take the name between the check and the
// write; the repository then reports the database's verdict.
func TestRenameCategory_DuplicateReportedByTheRepositoryIsStillA422(t *testing.T) {
	cs := &racingCategories{fakeCategories: newFakeCategories(activeCat)}
	_, err := NewCategoryService(cs, sequentialIDs("cat")).RenameCategory(context.Background(), activeCat.ID, in.RenameCategoryCommand{Name: "Video"})
	if !errors.Is(err, in.ErrCategoryNameTaken) {
		t.Fatalf("expected ErrCategoryNameTaken, got %v", err)
	}
}

type racingCategories struct{ *fakeCategories }

func (r *racingCategories) Update(context.Context, model.Category) error {
	return out.ErrDuplicateActiveName
}

func TestDeactivateCategory_WithoutActiveProductsDeactivates(t *testing.T) {
	cs := newFakeCategories(activeCat)
	res, err := newCategoryService(cs).DeactivateCategory(context.Background(), activeCat.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Category.Active || cs.byID[activeCat.ID].Active {
		t.Error("expected the category to be inactive")
	}
}

func TestDeactivateCategory_WithActiveProductsIsABusinessRuleViolation(t *testing.T) {
	cs := newFakeCategories(activeCat)
	cs.withProducts[activeCat.ID] = true
	_, err := newCategoryService(cs).DeactivateCategory(context.Background(), activeCat.ID)
	if !errors.Is(err, in.ErrCategoryHasActiveProducts) {
		t.Fatalf("expected ErrCategoryHasActiveProducts, got %v", err)
	}
	if cs.updates != 0 || !cs.byID[activeCat.ID].Active {
		t.Error("the category must stay active")
	}
}

func TestDeactivateCategory_AlreadyInactiveSucceedsWithoutWriting(t *testing.T) {
	cs := newFakeCategories(inactiveCat)
	res, err := newCategoryService(cs).DeactivateCategory(context.Background(), inactiveCat.ID)
	if err != nil {
		t.Fatalf("deactivating an inactive category must succeed, got %v", err)
	}
	if res.Category != inactiveCat || cs.updates != 0 {
		t.Errorf("got %+v with %d writes", res.Category, cs.updates)
	}
}

func TestDeactivateCategory_MissingIsNotFound(t *testing.T) {
	_, err := newCategoryService(newFakeCategories()).DeactivateCategory(context.Background(), "nope")
	if !errors.Is(err, in.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
}
