package api

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/events"
	"github.com/sorenhoang/go-observability-lab/internal/store"
)

type fakeStore struct {
	users    []store.User
	products []store.Product
	nextID   atomic.Int64
}

func newFakeStore() *fakeStore {
	st := &fakeStore{
		users: []store.User{
			{ID: 1, Name: "Ada"},
			{ID: 2, Name: "Alan"},
			{ID: 3, Name: "Grace"},
		},
		products: []store.Product{
			{ID: 1, Name: "Widget", Price: 9.99},
			{ID: 2, Name: "Gadget", Price: 19.99},
			{ID: 3, Name: "Gizmo", Price: 4.50},
		},
	}
	st.nextID.Store(100)
	return st
}

func (s *fakeStore) Users(context.Context) ([]store.User, error) {
	return append([]store.User(nil), s.users...), nil
}

func (s *fakeStore) Products(context.Context) ([]store.Product, error) {
	return append([]store.Product(nil), s.products...), nil
}

func (s *fakeStore) ProductExists(_ context.Context, id int) (bool, error) {
	for _, p := range s.products {
		if p.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) CreateOrder(context.Context, int, int) (int64, error) {
	return s.nextID.Add(1), nil
}

type passthroughCache struct{}

func (passthroughCache) Products(ctx context.Context, load func(context.Context) ([]store.Product, error)) ([]store.Product, error) {
	return load(ctx)
}

type recordingPublisher struct {
	events []events.OrderEvent
}

func (p *recordingPublisher) PublishOrder(_ context.Context, event events.OrderEvent) {
	p.events = append(p.events, event)
}

func TestCreateOrderPublishesOrderEvent(t *testing.T) {
	publisher := &recordingPublisher{}
	router := testRouterWithPublisher(publisher)

	rec := doReqWithRouter(t, router, "POST", "/orders", `{"product_id":1,"qty":2}`)
	if rec.Code != 201 {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.events))
	}
	got := publisher.events[0]
	if got.OrderID <= 0 || got.ProductID != 1 || got.Qty != 2 {
		t.Fatalf("event = %+v, want created order details", got)
	}
}
