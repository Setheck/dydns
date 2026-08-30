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
	dnsNamesiloInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "dns_namesilo_info",
		Help:      "Information about registered namesilo dns, always 1.",
	}, []string{"domain", "host", "public_ip"})
	dnsNamesiloUpdatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_namesilo_updates_total",
		Help:      "Total number of DNS updates.",
	})
	dnsNamesiloUpdateErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_namesilo_update_errors_total",
		Help:      "Total number of DNS update errors.",
	})
	dnsNamesiloListRecordsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_namesilo_list_records_total",
		Help:      "Total number of DNS records listed by Namesilo.",
	})
	dnsNamesiloListRecordsErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dns_namesilo_list_records_errors_total",
		Help:      "Total number of DNS record listing errors.",
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
