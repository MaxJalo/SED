package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

func main() {
	port := os.Getenv("MOCK_EXTERNAL_PORT")
	if port == "" {
		port = "8092"
	}

	var onecFail atomic.Bool
	if os.Getenv("MOCK_ONEC_FAIL_MODE") == "true" {
		onecFail.Store(true)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "mock-external", "onec_fail": onecFail.Load()})
	})
	mux.HandleFunc("POST /dev/onec/fail-mode/{mode}", func(w http.ResponseWriter, r *http.Request) {
		mode := r.PathValue("mode") == "on"
		onecFail.Store(mode)
		writeJSON(w, http.StatusOK, map[string]any{"onec_fail": mode})
	})
	mux.HandleFunc("POST /api/v1/platform-tasks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]string{"id": "pt-mock-1", "status": "created"})
	})
	mux.HandleFunc("POST /api/v1/notifications", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	})
	mux.HandleFunc("POST /api/v1/external-orders", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]string{"id": "eo-mock-1", "status": "created"})
	})
	mux.HandleFunc("POST /onec/documents", func(w http.ResponseWriter, _ *http.Request) {
		if onecFail.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulated 1C outage"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"onec_ref": "1c-mock-ref-001", "status": "synced"})
	})

	log.Printf("mock-external listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("mock-external failed: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
