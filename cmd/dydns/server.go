package main

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// readiness records when the last update cycle succeeded. It is written by the
// update loop and read by the /readyz handler.
type readiness struct {
	lastSuccess atomic.Int64
}

func (r *readiness) markSuccess(t time.Time) { r.lastSuccess.Store(t.Unix()) }

func (r *readiness) ready() bool { return r.lastSuccess.Load() > 0 }

// newHandler builds the observability endpoints.
//
// /healthz deliberately reports healthy for as long as the process is running:
// a NameSilo outage should raise an alert off last_success_timestamp_seconds,
// not crashloop the pod. /readyz is the one that gates on a successful cycle.
func newHandler(ready *readiness) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeOK(w)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.ready() {
			http.Error(w, "no successful update cycle yet", http.StatusServiceUnavailable)
			return
		}
		writeOK(w)
	})

	return mux
}

// startMetricsServer serves the observability endpoints on addr.
func startMetricsServer(addr string, ready *readiness) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(ready),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("metrics server listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Str("addr", addr).Msg("metrics server failed")
		}
	}()

	return srv
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
