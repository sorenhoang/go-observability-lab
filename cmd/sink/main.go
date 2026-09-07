package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type amPayload struct {
	Status string `json:"status"`
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"alerts"`
}

func newWebhookHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		var p amPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		for _, a := range p.Alerts {
			logger.Info("alert",
				"status", a.Status,
				"name", a.Labels["alertname"],
				"severity", a.Labels["severity"],
				"summary", a.Annotations["summary"],
			)
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	srv := &http.Server{
		Addr:              ":9000",
		Handler:           newWebhookHandler(logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("webhook sink listening", "addr", ":9000")
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("sink stopped", "err", err)
		os.Exit(1)
	}
}
