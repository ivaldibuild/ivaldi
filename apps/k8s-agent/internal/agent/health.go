package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The conditions /readyz requires.
const (
	conditionEnrolled = "enrolled"
	conditionLease    = "lease"
	// conditionTargets is met once the Targets are fetched and the
	// informers for their namespaces have synced.
	conditionTargets = "targets"
)

// readiness holds the conditions the agent must meet to be ready. Every
// condition starts unmet.
type readiness struct {
	mu         sync.Mutex
	conditions map[string]bool
}

func newReadiness(names ...string) *readiness {
	r := &readiness{conditions: map[string]bool{}}
	for _, name := range names {
		r.conditions[name] = false
	}
	return r
}

func (r *readiness) set(name string, met bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conditions[name] = met
}

// unmet returns the conditions not met, sorted.
func (r *readiness) unmet() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for name, met := range r.conditions {
		if !met {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func (r *readiness) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if unmet := r.unmet(); len(unmet) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready: "+strings.Join(unmet, ", "))
			return
		}
		_, _ = fmt.Fprintln(w, "ok")
	})
	return mux
}

// serveHealth serves the kubelet's probes on address until ctx ends.
func serveHealth(ctx context.Context, address string, r *readiness) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	server := &http.Server{Handler: r.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("health server: %w", err)
	}
	return nil
}
