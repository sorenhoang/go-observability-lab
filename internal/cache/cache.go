package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
	"github.com/sorenhoang/go-observability-lab/internal/obs"
	"github.com/sorenhoang/go-observability-lab/internal/store"
	"go.opentelemetry.io/otel/attribute"
)

const productsKey = "products"

type Cache struct {
	rdb     *redis.Client
	metrics *metrics.Metrics
	enabled bool
}

func New(addr string, m *metrics.Metrics) *Cache {
	return &Cache{
		rdb: redis.NewClient(&redis.Options{
			Addr: addr,
		}),
		metrics: m,
		enabled: true,
	}
}

func Disabled(m *metrics.Metrics) *Cache {
	return &Cache{metrics: m}
}

func (c *Cache) Ping(ctx context.Context) error {
	if !c.enabled {
		return nil
	}
	return c.rdb.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

func (c *Cache) Flush(ctx context.Context) error {
	if !c.enabled {
		return nil
	}
	return c.rdb.FlushDB(ctx).Err()
}

func (c *Cache) Products(ctx context.Context, load func(context.Context) ([]store.Product, error)) ([]store.Product, error) {
	ctx, span := obs.Tracer().Start(ctx, "cache.get products")
	defer span.End()

	if !c.enabled {
		span.SetAttributes(attribute.Bool("cache.hit", false))
		c.metrics.CacheResult("miss")
		return load(ctx)
	}

	b, err := c.rdb.Get(ctx, productsKey).Bytes()
	if err == nil {
		var products []store.Product
		if err := json.Unmarshal(b, &products); err == nil {
			span.SetAttributes(attribute.Bool("cache.hit", true))
			c.metrics.CacheResult("hit")
			return products, nil
		}
		c.metrics.CacheError()
		obs.LoggerFrom(ctx).Warn("products cache decode failed", "err", err)
	} else if err != redis.Nil {
		c.metrics.CacheError()
		obs.LoggerFrom(ctx).Warn("products cache unavailable; falling through", "err", err)
	}

	span.SetAttributes(attribute.Bool("cache.hit", false))
	c.metrics.CacheResult("miss")
	products, err := load(ctx)
	if err != nil {
		return nil, err
	}
	b, err = json.Marshal(products)
	if err != nil {
		c.metrics.CacheError()
		return products, nil
	}
	if err := c.rdb.Set(ctx, productsKey, b, 30*time.Second).Err(); err != nil {
		c.metrics.CacheError()
		obs.LoggerFrom(ctx).Warn("products cache set failed", "err", err)
	}
	return products, nil
}
