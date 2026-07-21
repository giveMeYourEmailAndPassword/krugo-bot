package main

import (
	"bytes"
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

var requiredScripts = []string{
	"pb_helper.sh", "add_supplier.sh", "cancel_supplier.sh",
	"change_supplier.sh", "create_correction.sh",
	"create_finance_request.sh", "create_operator_request.sh",
	"create_payment.sh", "create_refund.sh",
}

// validateTools checks that all required scripts exist and are executable.
// Returns nil if all OK, or a slice of problem descriptions.
func validateTools(toolsDir string) []string {
	var problems []string
	for _, s := range requiredScripts {
		path := toolsDir + "/" + s
		info, err := os.Stat(path)
		if err != nil {
			problems = append(problems, s+" missing")
			continue
		}
		if info.Mode()&0o111 == 0 && s != "pb_helper.sh" {
			problems = append(problems, s+" not executable")
		}
	}
	return problems
}

func main() {
	apiKey := os.Getenv("HERMES_BRIDGE_KEY")
	if apiKey == "" {
		log.Fatal("HERMES_BRIDGE_KEY is required")
	}

	// Configurable Hermes timeout. Default 3 minutes; override via HERMES_TIMEOUT.
	// Prevents 15-minute hangs when Hermes is slow or stuck.
	hermesTimeout := 3 * time.Minute
	if t := os.Getenv("HERMES_TIMEOUT"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			hermesTimeout = d
		} else {
			log.Printf("invalid HERMES_TIMEOUT %q, using default 3m", t)
		}
	}
	log.Printf("hermes timeout: %v", hermesTimeout)

	toolsDir := os.Getenv("HERMES_HOME") + "/skills/contracts/tools"

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		problems := validateTools(toolsDir)
		if len(problems) > 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"status":   "unhealthy",
				"problems": problems,
				"path":     toolsDir,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "healthy",
			"tools":  len(requiredScripts),
			"path":   toolsDir,
		})
	})

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

		// Preflight: verify all required tool scripts are mounted and executable.
		// Fail fast — prevents long timeout when tools/ is empty or partial.
		problems := validateTools(toolsDir)
		if len(problems) > 0 {
			log.Printf("preflight FAIL: %v at %s", problems, toolsDir)
			writeJSON(w, http.StatusInternalServerError, response{
				Error:  "ОШИБКА: инструменты не смонтированы. Нужен redeploy контейнера.",
				Status: 500,
			})
			return
		}
		log.Printf("preflight OK: %d scripts verified at %s", len(requiredScripts), toolsDir)

		log.Printf("oneshot: %d bytes", len(req.Prompt))

		mu.Lock()
		defer mu.Unlock()

		ctx, cancel := context.WithTimeout(r.Context(), hermesTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "hermes", "-z", req.Prompt)
		cmd.Env = append(os.Environ(), "HERMES_HOME="+os.Getenv("HERMES_HOME"))

		// Capture stdout (user response) and stderr (TOOL_TRACE diagnostics) separately.
		var stdoutBuf, stderrBuf bytes.Buffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		if err := cmd.Run(); err != nil {
			// Log stderr for diagnostics (TOOL_TRACE, script errors).
			for _, line := range bytes.Split(stderrBuf.Bytes(), []byte{'\n'}) {
				if len(line) > 0 {
					log.Printf("hermes stderr: %s", line)
				}
			}
			log.Printf("hermes error: %v", err)
			writeJSON(w, http.StatusInternalServerError, response{
				Error:  "hermes failed",
				Status: 500,
			})
			return
		}

		// Log stderr for diagnostics on success too (TOOL_TRACE emitted on success).
		for _, line := range bytes.Split(stderrBuf.Bytes(), []byte{'\n'}) {
			if len(line) > 0 {
				log.Printf("hermes stderr: %s", line)
			}
		}

		log.Printf("response: %d bytes", stdoutBuf.Len())
		writeJSON(w, http.StatusOK, response{Text: stdoutBuf.String(), Status: 200})
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
