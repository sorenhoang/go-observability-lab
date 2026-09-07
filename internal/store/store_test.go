package store

import (
	"context"
	"os"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

func TestStoreListsSeededProductsAndCreatesOrders(t *testing.T) {
	dsn := os.Getenv("API_DATABASE_URL")
	if dsn == "" {
		t.Skip("API_DATABASE_URL unset; skipping Postgres integration test")
	}

	ctx := context.Background()
	st, err := Open(ctx, dsn, metrics.New())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	products, err := st.Products(ctx)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != 3 {
		t.Fatalf("len(products) = %d, want 3", len(products))
	}

	id, err := st.CreateOrder(ctx, products[0].ID, 2)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if id <= 0 {
		t.Fatalf("order id = %d, want positive", id)
	}
}
