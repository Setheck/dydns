package main

import (
	"runtime"
	"runtime/debug"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "dydns"

// Stages of a single update cycle. Each value corresponds 1:1 to an early
// return in updateDynamicDNS, so the label set is small and fixed.
const (
	stagePublicIP          = "public_ip"
	stageListRecords       = "list_records"
	stageListRecordsReply  = "list_records_reply"
	stageRecordNotFound    = "record_not_found"
	stageUpdateRecord      = "update_record"
	stageUpdateRecordReply = "update_record_reply"
)

var allStages = []string{
	stagePublicIP,
	stageListRecords,
	stageListRecordsReply,
	stageRecordNotFound,
	stageUpdateRecord,
	stageUpdateRecordReply,
}

// Overridden at build time via -ldflags, with a ReadBuildInfo fallback.
var (
	version = "dev"
	commit  = "none"
)

// durationBuckets spans well past the default 30s update timeout; DefBuckets
// tops out at 10s, which would lump every slow call into +Inf.
var durationBuckets = prometheus.ExponentialBuckets(0.05, 2, 11)

var (
	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information for the running binary, always 1.",
	}, []string{"version", "commit", "go_version"})

	targetInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "target_info",
		Help:      "The domain and host this instance keeps up to date, always 1.",
	}, []string{"domain", "host"})

	publicIPInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "public_ip_info",
		Help:      "The most recently observed public IP, always 1. Reset each check so only one series exists.",
	}, []string{"ip"})

	updateCyclesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "update_cycles_total",
		Help:      "Total number of update cycles attempted.",
	})

	updateCycleErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "update_cycle_errors_total",
		Help:      "Total number of failed update cycles, by the stage that failed.",
	}, []string{"stage"})

	updateCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "update_cycle_duration_seconds",
		Help:      "Duration of a full update cycle.",
		Buckets:   durationBuckets,
	})

	lastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_success_timestamp_seconds",
		Help:      "Unix timestamp of the last successful update cycle. 0 if none has succeeded yet.",
	})

	lastChangeTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_change_timestamp_seconds",
		Help:      "Unix timestamp of the last time the DNS record was actually rewritten.",
	})

	consecutiveFailures = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "consecutive_failures",
		Help:      "Number of update cycles that have failed back-to-back. Reset to 0 on success.",
	})

	recordUpToDate = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "record_up_to_date",
		Help:      "1 if the DNS record matches the observed public IP, 0 otherwise.",
	})

	recordChangesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "record_changes_total",
		Help:      "Total number of successful DNS record rewrites.",
	})

	namesiloRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "namesilo_requests_total",
		Help:      `Total NameSilo API requests, by operation and reply code ("error" for transport or decode failures).`,
	}, []string{"operation", "reply_code"})

	namesiloRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "namesilo_request_duration_seconds",
		Help:      "Duration of NameSilo API requests.",
		Buckets:   durationBuckets,
	}, []string{"operation"})

	publicIPChecksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "public_ip_checks_total",
		Help:      "Total public IP lookups, by result.",
	}, []string{"result"})

	publicIPCheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "public_ip_check_duration_seconds",
		Help:      "Duration of public IP lookups.",
		Buckets:   durationBuckets,
	})
)

// initMetrics sets the static info gauges and materializes every known label
// combination at zero, so rate() and absent() behave before the first event.
func initMetrics(version, commit, domain, host string) {
	buildInfo.WithLabelValues(version, commit, runtime.Version()).Set(1)
	targetInfo.WithLabelValues(domain, host).Set(1)

	for _, stage := range allStages {
		updateCycleErrorsTotal.WithLabelValues(stage)
	}
	for _, result := range []string{resultSuccess, resultError} {
		publicIPChecksTotal.WithLabelValues(result)
	}
	for _, op := range []string{operationListRecords, operationUpdateRecord} {
		namesiloRequestDuration.WithLabelValues(op)
	}
}

// buildVersion resolves the version and commit, preferring ldflags values and
// falling back to the Go module build info stamped by the toolchain.
func buildVersion() (string, string) {
	v, c := version, commit
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c
	}
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	if c == "none" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				c = s.Value
				break
			}
		}
	}
	return v, c
}
