package model

import "errors"

var ErrCategoryNameRequired = errors.New("name is required")

type Category struct {
	ID     string
	Name   string
	Active bool
}

func NewCategory(id, name string) (Category, error) {
	if name == "" {
		return Category{}, ErrCategoryNameRequired
	}
	return Category{ID: id, Name: name, Active: true}, nil
}

func (c *Category) Rename(name string) error {
	if name == "" {
		return ErrCategoryNameRequired
	}
	c.Name = name
	return nil
}

func (c *Category) Deactivate() { c.Active = false }
