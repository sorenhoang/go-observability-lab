package cache

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/metrics"
	"github.com/sorenhoang/go-observability-lab/internal/store"
)

func TestCacheDisabledFallsThroughToLoader(t *testing.T) {
	c := Disabled(metrics.New())
	calls := 0

	got, err := c.Products(context.Background(), func(context.Context) ([]store.Product, error) {
		calls++
		return []store.Product{{ID: 1, Name: "Widget", Price: 9.99}}, nil
	})
	if err != nil {
		t.Fatalf("Products returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("loader calls = %d, want 1", calls)
	}
	if len(got) != 1 || got[0].Name != "Widget" {
		t.Fatalf("products = %+v, want Widget", got)
	}
}

func TestCacheReturnsLoaderErrorWhenSourceFails(t *testing.T) {
	c := Disabled(metrics.New())
	wantErr := errors.New("db down")

	_, err := c.Products(context.Background(), func(context.Context) ([]store.Product, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestCacheProductsHitMissAndRedisDown(t *testing.T) {
	addr := os.Getenv("API_REDIS_ADDR")
	if addr == "" {
		t.Skip("API_REDIS_ADDR unset; skipping Redis integration test")
	}

	ctx := context.Background()
	c := New(addr, metrics.New())
	defer c.Close()
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("flush redis: %v", err)
	}

	calls := 0
	load := func(context.Context) ([]store.Product, error) {
		calls++
		return []store.Product{{ID: 1, Name: "Widget", Price: 9.99}}, nil
	}

	if _, err := c.Products(ctx, load); err != nil {
		t.Fatalf("cold products: %v", err)
	}
	if _, err := c.Products(ctx, load); err != nil {
		t.Fatalf("warm products: %v", err)
	}
	if calls != 1 {
		t.Fatalf("loader calls = %d, want 1 after cache hit", calls)
	}

	down := New("127.0.0.1:1", metrics.New())
	defer down.Close()
	if _, err := down.Products(ctx, load); err != nil {
		t.Fatalf("redis down should fall through, got %v", err)
	}
}
