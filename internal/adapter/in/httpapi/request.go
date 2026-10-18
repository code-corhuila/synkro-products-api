package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	defaultPage  = 1
	defaultLimit = 20
	maxLimit     = 100
)

// decodeBody reads a JSON object into dst. A malformed body or a value of
// the wrong type answers with one detail, naming the field when known.
func decodeBody(r *http.Request, dst any) []errorDetail {
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return []errorDetail{{Field: typeErr.Field, Message: "must be of type " + typeErr.Type.String()}}
	}
	return []errorDetail{{Field: "body", Message: "must be a valid JSON object"}}
}

func requireIdempotencyKey(r *http.Request) (string, []errorDetail) {
	key := r.Header.Get("Idempotency-Key")
	if n := utf8.RuneCountInString(key); n < 8 || n > 128 {
		return "", []errorDetail{{Field: "Idempotency-Key", Message: "required, 8 to 128 characters"}}
	}
	return key, nil
}

// canonicalUUID accepts only the 36-character hyphenated form and returns
// it lowercased, so the in-memory store and PostgreSQL agree on identity.
func canonicalUUID(s string) (string, bool) {
	if len(s) != 36 {
		return "", false
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return "", false
	}
	return id.String(), true
}

// pathID returns the {id} segment, or false when it cannot name any
// resource (the caller answers 404, as the contract lists no 400 there).
func pathID(r *http.Request) (string, bool) {
	return canonicalUUID(r.PathValue("id"))
}

func checkName(name *string, max int) *errorDetail {
	switch {
	case name == nil || *name == "":
		return &errorDetail{Field: "name", Message: "required"}
	case utf8.RuneCountInString(*name) > max:
		return &errorDetail{Field: "name", Message: fmt.Sprintf("at most %d characters", max)}
	}
	return nil
}

// query walks the query string once, rejecting any parameter the
// endpoint does not declare (_shared.yaml BadRequest: "unknown filter").
type query struct {
	values  url.Values
	details []errorDetail
}

func newQuery(r *http.Request, allowed ...string) *query {
	q := &query{values: r.URL.Query()}
	known := map[string]bool{"page": true, "limit": true}
	for _, a := range allowed {
		known[a] = true
	}
	for name := range q.values {
		if !known[name] {
			q.details = append(q.details, errorDetail{Field: name, Message: "unknown query parameter"})
		}
	}
	return q
}

func (q *query) fail(field, msg string) {
	q.details = append(q.details, errorDetail{Field: field, Message: msg})
}

func (q *query) intIn(name string, def, min, max int) int {
	if !q.values.Has(name) {
		return def
	}
	n, err := strconv.Atoi(q.values.Get(name))
	if err != nil || n < min || n > max {
		q.fail(name, fmt.Sprintf("must be an integer from %d to %d", min, max))
		return def
	}
	return n
}

func (q *query) page() (page, limit int) {
	return q.intIn("page", defaultPage, 1, int(^uint32(0)>>1)), q.intIn("limit", defaultLimit, 1, maxLimit)
}

func (q *query) optBool(name string) *bool {
	if !q.values.Has(name) {
		return nil
	}
	b, err := strconv.ParseBool(q.values.Get(name))
	if err != nil {
		q.fail(name, "must be true or false")
		return nil
	}
	return &b
}

func (q *query) optString(name string) *string {
	if !q.values.Has(name) {
		return nil
	}
	s := q.values.Get(name)
	return &s
}

func (q *query) optUUID(name string) *string {
	if !q.values.Has(name) {
		return nil
	}
	id, ok := canonicalUUID(q.values.Get(name))
	if !ok {
		q.fail(name, "must be a UUID")
		return nil
	}
	return &id
}

func (q *query) optNonNegativeInt(name string) *int {
	if !q.values.Has(name) {
		return nil
	}
	n, err := strconv.Atoi(q.values.Get(name))
	if err != nil || n < 0 {
		q.fail(name, "must be an integer of 0 or more")
		return nil
	}
	return &n
}

type pageMetaResponse struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

func newPageMeta(page, limit, total int) pageMetaResponse {
	return pageMetaResponse{Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit}
}
