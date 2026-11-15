package httpapi

import (
	"net/http"
	"testing"
	"time"
)

type priceChangeJSON struct {
	HistoryID     string `json:"historyId"`
	ProductID     string `json:"productId"`
	Sequence      int64  `json:"sequence"`
	OldPriceCents int64  `json:"oldPriceCents"`
	NewPriceCents int64  `json:"newPriceCents"`
	ChangedBy     string `json:"changedBy"`
	ChangedAt     string `json:"changedAt"`
}

func exercisePriceHistoryHTTP(t *testing.T, api *testAPI) string {
	t.Helper()
	const first = "0192a000-0000-7000-8000-0000000000b2"
	const second = "0192a000-0000-7000-8000-0000000000c3"
	token := func(sub string) string {
		return tokenWithClaims(sub, []string{"INVENTORY"}, []string{"products:read", "products:write"})
	}
	c := api.createCategory("Price history")
	p := api.createProduct("Mouse", 100, c.CategoryID)
	path := "/api/v1/products/" + p.ProductID
	put := func(price int64, raw string) apiResponse {
		return api.do("PUT", path, map[string]any{"name": "Mouse", "priceCents": price, "categoryId": c.CategoryID, "changedBy": testSub, "changedAt": "1900-01-01T00:00:00Z"}, as(raw)...)
	}
	for i, raw := range []string{token(first), token(second)} {
		if r := put(int64(200+i*100), raw); r.status != http.StatusOK {
			t.Fatalf("PUT %d: %d %s", i, r.status, r.body)
		}
	}
	if r := put(300, token(first)); r.status != http.StatusOK {
		t.Fatal(r.status, string(r.body))
	}
	forged := signedWith(otherKey, map[string]any{"alg": "RS256"}, validClaims(first, []string{"INVENTORY"}, []string{"products:write", "products:read"}))
	if r := put(1, forged); r.status != 401 {
		t.Fatal(r.status)
	}
	r := api.do("GET", path+"/price-history", nil, as(token(first))...)
	if r.status != 200 {
		t.Fatalf("history %d %s", r.status, r.body)
	}
	var history struct {
		Data []priceChangeJSON `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	r.decode(t, &history)
	if len(history.Data) != 2 || history.Meta.Total != 2 {
		t.Fatalf("history %s", r.body)
	}
	for i, h := range history.Data {
		authors := []string{second, first}
		old := []int64{200, 100}
		next := []int64{300, 200}
		at, err := time.Parse(time.RFC3339Nano, h.ChangedAt)
		if h.HistoryID == "" || h.ProductID != p.ProductID || h.Sequence != int64(2-i) || h.ChangedBy != authors[i] || h.OldPriceCents != old[i] || h.NewPriceCents != next[i] || err != nil || at.Location() != time.UTC || at.Year() != time.Now().UTC().Year() {
			t.Fatalf("bad event %+v %v", h, err)
		}
	}
	for _, q := range []string{"?limit=101", "?page=0", "?limit=0", "?unknown=1"} {
		if r := api.do("GET", path+"/price-history"+q, nil); r.status != 400 {
			t.Fatalf("%s: %d", q, r.status)
		}
	}
	for _, raw := range []string{salespersonToken, workerToken, tokenWithClaims(first, []string{"INVENTORY"}, nil)} {
		if r := api.do("GET", path+"/price-history", nil, as(raw)...); r.status != 403 {
			t.Fatalf("forbidden %d %s", r.status, r.body)
		}
	}
	if r := api.do("GET", path+"/price-history", nil, as(forged)...); r.status != 401 {
		t.Fatal(r.status)
	}
	if r := api.do("GET", "/api/v1/products/00000000-0000-0000-0000-000000000001/price-history", nil); r.status != 404 {
		t.Fatal(r.status)
	}
	if r := api.do("GET", "/api/v1/products/invalid/price-history", nil); r.status != 404 {
		t.Fatal(r.status)
	}
	if r := api.do("GET", path+"/price-history?limit=1&page=2", nil); r.status != 200 {
		t.Fatal(r.status)
	} else {
		r.decode(t, &history)
		if len(history.Data) != 1 || history.Data[0].Sequence != 1 {
			t.Fatalf("page %s", r.body)
		}
	}
	var product productJSON
	api.do("GET", path, nil).decode(t, &product)
	if product.PriceCents != 300 {
		t.Fatalf("rejected PUT changed price: %+v", product)
	}
	return p.ProductID
}

func TestPriceHistoryHTTP_TwoInventoryAuthorsNoopAndRejections(t *testing.T) {
	exercisePriceHistoryHTTP(t, newTestAPI(t))
}
