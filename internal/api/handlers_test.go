package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doReq(t *testing.T, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, r)
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, req)
	return rec
}

func TestUsersReturnsFakeUsers(t *testing.T) {
	rec := doReq(t, http.MethodGet, "/users", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got []user
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(got))
	}
}

func TestProductsReturnsFakeProducts(t *testing.T) {
	rec := doReq(t, http.MethodGet, "/products", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got []product
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(products) = %d, want 3", len(got))
	}
}

func TestCreateOrderReturnsCreatedOrder(t *testing.T) {
	rec := doReq(t, http.MethodPost, "/orders", `{"product_id":1,"qty":2}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var got orderResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.OrderID <= 0 {
		t.Fatalf("order_id = %d, want positive", got.OrderID)
	}
	if got.ProductID != 1 || got.Qty != 2 {
		t.Fatalf("order = %+v, want product_id=1 qty=2", got)
	}
}

func TestCreateOrderRejectsNonPositiveQuantity(t *testing.T) {
	rec := doReq(t, http.MethodPost, "/orders", `{"product_id":1,"qty":0}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateOrderRejectsMissingProduct(t *testing.T) {
	rec := doReq(t, http.MethodPost, "/orders", `{"product_id":999,"qty":1}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCreateOrderRejectsMalformedJSON(t *testing.T) {
	rec := doReq(t, http.MethodPost, "/orders", `{bad json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateOrderRejectsUnknownFields(t *testing.T) {
	rec := doReq(t, http.MethodPost, "/orders", `{"product_id":1,"qty":1,"x":1}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSlowHonorsMaxMSOverride(t *testing.T) {
	start := time.Now()
	rec := doReq(t, http.MethodGet, "/slow?max_ms=10", "")
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if elapsed >= time.Second {
		t.Fatalf("GET /slow?max_ms=10 took %s, want under 1s", elapsed)
	}

	var body struct {
		SleptMs int `json:"slept_ms"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.SleptMs < 0 || body.SleptMs >= 10 {
		t.Fatalf("slept_ms = %d, want in [0,10)", body.SleptMs)
	}
}

func TestSlowStopsOnClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/slow?max_ms=5000", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		testRouter().ServeHTTP(rec, req)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after context cancel")
	}
}

func TestErrorRateOneAlwaysFails(t *testing.T) {
	for range 20 {
		rec := doReq(t, http.MethodGet, "/error?rate=1", "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	}
}

func TestErrorRateZeroAlwaysSucceeds(t *testing.T) {
	for range 20 {
		rec := doReq(t, http.MethodGet, "/error?rate=0", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	}
}

func TestWrongMethodReturns405(t *testing.T) {
	rec := doReq(t, http.MethodPut, "/users", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, routePattern(r))
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users?page=2", nil))

	if rec.Body.String() != "/users" {
		t.Fatalf("got %q, want /users", rec.Body.String())
	}
}
