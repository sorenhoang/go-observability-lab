package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type Store struct {
	db      *sql.DB
	metrics *metrics.Metrics
}

func Open(ctx context.Context, dsn string, m *metrics.Metrics) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	m.RegisterDBStats(db)
	return &Store{db: db, metrics: m}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	var users []User
	err := s.timed(ctx, "users.list", func(ctx context.Context) error {
		rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM users ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Name); err != nil {
				return err
			}
			users = append(users, u)
		}
		return rows.Err()
	})
	return users, err
}

func (s *Store) Products(ctx context.Context) ([]Product, error) {
	var products []Product
	err := s.timed(ctx, "products.list", func(ctx context.Context) error {
		rows, err := s.db.QueryContext(ctx, `SELECT id, name, price FROM products ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Product
			if err := rows.Scan(&p.ID, &p.Name, &p.Price); err != nil {
				return err
			}
			products = append(products, p)
		}
		return rows.Err()
	})
	return products, err
}

func (s *Store) ProductExists(ctx context.Context, id int) (bool, error) {
	var exists bool
	err := s.timed(ctx, "products.exists", func(ctx context.Context) error {
		return s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, id).Scan(&exists)
	})
	return exists, err
}

func (s *Store) CreateOrder(ctx context.Context, productID, qty int) (int64, error) {
	var id int64
	err := s.timed(ctx, "orders.insert", func(ctx context.Context) error {
		return s.db.QueryRowContext(
			ctx,
			`INSERT INTO orders (product_id, qty) VALUES ($1, $2) RETURNING id`,
			productID,
			qty,
		).Scan(&id)
	})
	return id, err
}

func (s *Store) timed(ctx context.Context, name string, fn func(context.Context) error) error {
	start := time.Now()
	err := fn(ctx)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.metrics.ObserveDBQuery(name, status, time.Since(start))
	return err
}
