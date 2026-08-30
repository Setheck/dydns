package main

import (
	"net/http"
	"os"

	prometheus "github.com/prometheus/client_golang/prometheus"
	promauto "github.com/prometheus/client_golang/prometheus/promauto"
	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	namespace       = "dydns"
	dnsUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_updates_total",
		Help:      "Total number of DNS updates.",
	})
	dnsUpdateErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_update_errors_total",
		Help:      "Total number of DNS update errors.",
	})
)

func StartMetricsServer() {
	go func() {
		port := os.Getenv("DYDNS_METRICS_PORT")
		if port == "" {
			port = "8080"
		}
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":"+port, nil)
	}()
}
