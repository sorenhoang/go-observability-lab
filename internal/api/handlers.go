package api

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/sorenhoang/go-observability-lab/internal/events"
)

type createOrderRequest struct {
	ProductID int `json:"product_id"`
	Qty       int `json:"qty"`
}

type orderResponse struct {
	OrderID   int64 `json:"order_id"`
	ProductID int   `json:"product_id"`
	Qty       int   `json:"qty"`
}

func (h *Handlers) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.Users(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list users failed")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handlers) handleProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.cache.Products(r.Context(), h.store.Products)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list products failed")
		return
	}
	writeJSON(w, http.StatusOK, products)
}

func (h *Handlers) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Qty <= 0 {
		writeError(w, http.StatusBadRequest, "qty must be positive")
		return
	}
	exists, err := h.store.ProductExists(r.Context(), req.ProductID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "check product failed")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}

	id, err := h.store.CreateOrder(r.Context(), req.ProductID, req.Qty)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create order failed")
		return
	}
	h.metrics.OrderCreated()
	h.events.PublishOrder(r.Context(), events.OrderEvent{
		OrderID:   id,
		ProductID: req.ProductID,
		Qty:       req.Qty,
		TS:        time.Now().UTC(),
	})
	writeJSON(w, http.StatusCreated, orderResponse{
		OrderID:   id,
		ProductID: req.ProductID,
		Qty:       req.Qty,
	})
}

func (h *Handlers) handleSlow(w http.ResponseWriter, r *http.Request) {
	maxMs := h.cfg.SlowMaxMs
	if v := r.URL.Query().Get("max_ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxMs = n
		}
	}

	minMs := h.cfg.SlowMinMs
	if minMs > maxMs {
		minMs = maxMs
	}

	d := minMs
	if maxMs > minMs {
		d += rand.IntN(maxMs - minMs)
	}

	// Respect client disconnect / server shutdown: an uninterruptible Sleep
	// would keep a goroutine (and, from Phase 2, the in-progress gauge) tied
	// up after the caller has gone.
	select {
	case <-time.After(time.Duration(d) * time.Millisecond):
		writeJSON(w, http.StatusOK, map[string]int{"slept_ms": d})
	case <-r.Context().Done():
	}
}

func (h *Handlers) handleError(w http.ResponseWriter, r *http.Request) {
	rate := h.cfg.ErrorRate
	if v := r.URL.Query().Get("rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			rate = f
		}
	}

	if rand.Float64() < rate {
		writeError(w, http.StatusInternalServerError, "simulated failure")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
