package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

type request struct {
	Prompt string `json:"prompt"`
}

type response struct {
	Text   string `json:"text,omitempty"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status"`
}

var mu sync.Mutex

func main() {
	apiKey := os.Getenv("HERMES_BRIDGE_KEY")
	if apiKey == "" {
		log.Fatal("HERMES_BRIDGE_KEY is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/oneshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+apiKey)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, response{Error: "body too large", Status: 400})
			return
		}

		var req request
		if json.Unmarshal(body, &req) != nil || req.Prompt == "" {
			writeJSON(w, http.StatusBadRequest, response{Error: "bad request", Status: 400})
			return
		}

		log.Printf("oneshot: %d bytes", len(req.Prompt))

		mu.Lock()
		defer mu.Unlock()

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "hermes", "-z", req.Prompt)
		cmd.Env = append(os.Environ(), "HERMES_HOME="+os.Getenv("HERMES_HOME"))

		stdout, err := cmd.Output()
		if err != nil {
			log.Printf("hermes error: %v", err)
			writeJSON(w, http.StatusInternalServerError, response{
				Error:  "hermes failed",
				Status: 500,
			})
			return
		}

		log.Printf("response: %d bytes", len(stdout))
		writeJSON(w, http.StatusOK, response{Text: string(stdout), Status: 200})
	})

	srv := &http.Server{
		Addr:         "127.0.0.1:8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("hermes-proxy listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
