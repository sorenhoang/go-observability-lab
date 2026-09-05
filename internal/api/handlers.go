package api

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

type user struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type createOrderRequest struct {
	ProductID int `json:"product_id"`
	Qty       int `json:"qty"`
}

type orderResponse struct {
	OrderID   int64 `json:"order_id"`
	ProductID int   `json:"product_id"`
	Qty       int   `json:"qty"`
}

var users = []user{
	{ID: 1, Name: "Ada"},
	{ID: 2, Name: "Alan"},
	{ID: 3, Name: "Grace"},
}

var products = []product{
	{ID: 1, Name: "Widget", Price: 9.99},
	{ID: 2, Name: "Gadget", Price: 19.99},
	{ID: 3, Name: "Gizmo", Price: 4.50},
}

func handleUsers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, users)
}

func handleProducts(w http.ResponseWriter, _ *http.Request) {
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
	if !productExists(req.ProductID) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}

	id := h.orderSeq.Add(1)
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

func productExists(id int) bool {
	for _, p := range products {
		if p.ID == id {
			return true
		}
	}
	return false
}
