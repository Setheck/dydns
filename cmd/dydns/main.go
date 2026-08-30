package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
	"github.com/setheck/dydns/pkg/namesilo"
)

const (
	recordTypeA = "A"

	resultSuccess = "success"
	resultError   = "error"

	operationListRecords  = namesilo.OperationDnsListRecords
	operationUpdateRecord = namesilo.OperationDnsUpdateRecord

	shutdownTimeout = 5 * time.Second
)

type environment struct {
	NamesiloAPIKey         string        `envconfig:"NAMESILO_API_KEY" required:"true"`
	NamesiloDomain         string        `envconfig:"NAMESILO_DOMAIN" required:"true"`
	NamesiloHost           string        `envconfig:"NAMESILO_HOST" required:"true"`
	NamesiloUpdateInterval time.Duration `envconfig:"NAMESILO_UPDATE_INTERVAL" default:"24h"`
	NamesiloUpdateTimeout  time.Duration `envconfig:"NAMESILO_UPDATE_TIMEOUT" default:"30s"`
	NamesiloRecordTTL      int           `envconfig:"NAMESILO_RECORD_TTL" default:"7207"`
	NamesiloBaseURL        string        `envconfig:"NAMESILO_BASE_URL" default:"https://www.namesilo.com/api"`
	MetricsPort            int           `envconfig:"DYDNS_METRICS_PORT" default:"8080"`
	PublicIPURL            string        `envconfig:"PUBLIC_IP_URL" default:"https://ifconfig.me/ip"`
	LogLevel               string        `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat              string        `envconfig:"LOG_FORMAT" default:"auto"`
}

var log = newLogger("auto", "info")

// newLogger builds the logger. format is "auto", "json" or "console"; "auto"
// uses the human-readable console writer only when stdout is a terminal, so
// container logs stay machine-parseable.
func newLogger(format, level string) zerolog.Logger {
	var w io.Writer = os.Stdout
	if format == "console" || (format != "json" && isTerminal(os.Stdout)) {
		w = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	lvl, err := zerolog.ParseLevel(level)
	if err != nil || lvl == zerolog.NoLevel {
		lvl = zerolog.InfoLevel
	}

	return zerolog.New(w).Level(lvl).With().Timestamp().Logger()
}

// isTerminal reports whether f is a character device. Pipes and regular files
// are not, which is the case that matters here: it keeps container logs as
// JSON. Character devices other than a tty (/dev/null) also report true, so set
// LOG_FORMAT explicitly if that distinction matters.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	env := &environment{}
	if err := envconfig.Process("", env); err != nil {
		log.Fatal().Err(err).Msg("failed to process environment")
	}
	log = newLogger(env.LogFormat, env.LogLevel)

	version, commit := buildVersion()
	log.Info().
		Str("version", version).
		Str("commit", commit).
		Str("domain", env.NamesiloDomain).
		Str("host", env.NamesiloHost).
		Str("base_url", env.NamesiloBaseURL).
		Str("update_interval", env.NamesiloUpdateInterval.String()).
		Str("update_timeout", env.NamesiloUpdateTimeout.String()).
		Int("record_ttl", env.NamesiloRecordTTL).
		Msg("starting dydns")

	initMetrics(version, commit, env.NamesiloDomain, env.NamesiloHost)

	ready := &readiness{}
	srv := startMetricsServer(fmt.Sprintf(":%d", env.MetricsPort), ready)

	// One client for both NameSilo and the public IP lookup. The per-request
	// context carries the real deadline; the timeout here is a backstop.
	httpClient := &http.Client{Timeout: env.NamesiloUpdateTimeout}
	client := namesilo.New(env.NamesiloAPIKey,
		namesilo.WithHTTPClient(httpClient),
		namesilo.WithBaseURL(env.NamesiloBaseURL))

	updateOnInterval(ctx, client, updateConfig{
		domain:      env.NamesiloDomain,
		host:        env.NamesiloHost,
		interval:    env.NamesiloUpdateInterval,
		timeout:     env.NamesiloUpdateTimeout,
		ttl:         env.NamesiloRecordTTL,
		publicIPURL: env.PublicIPURL,
		httpClient:  httpClient,
	}, ready)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("metrics server shutdown failed")
	}
	log.Info().Msg("shutdown complete")
}

type updateConfig struct {
	domain      string
	host        string
	interval    time.Duration
	timeout     time.Duration
	ttl         int
	publicIPURL string
	httpClient  *http.Client
}

// stageError tags a failure with the update-cycle stage that produced it, so
// the loop can attribute it to a metric label in one place.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func stageErrf(stage, format string, a ...any) error {
	return &stageError{stage: stage, err: fmt.Errorf(format, a...)}
}

func updateOnInterval(ctx context.Context, client *namesilo.Client, cfg updateConfig, ready *readiness) {
	for {
		log.Info().Msg("updating dynamic DNS")

		start := time.Now()
		updateCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		err := updateDynamicDNS(updateCtx, client, cfg)
		cancel()

		updateCyclesTotal.Inc()
		updateCycleDuration.Observe(time.Since(start).Seconds())

		switch {
		case err == nil:
			now := time.Now()
			lastSuccessTimestamp.Set(float64(now.Unix()))
			consecutiveFailures.Set(0)
			ready.markSuccess(now)
		case ctx.Err() != nil:
			// The parent context was cancelled: this is a shutdown, not a
			// failure, so it must not count against the error metrics.
			log.Info().Msg("update interrupted by shutdown")
		default:
			consecutiveFailures.Inc()
			event := log.Error().Err(err)
			var se *stageError
			if errors.As(err, &se) {
				updateCycleErrorsTotal.WithLabelValues(se.stage).Inc()
				event = event.Str("stage", se.stage)
			}
			event.Msg("update cycle failed")
		}

		select {
		case <-ctx.Done():
			log.Info().Msg("shutting down")
			return
		case <-time.After(cfg.interval):
		}
	}
}

func updateDynamicDNS(ctx context.Context, client *namesilo.Client, cfg updateConfig) error {
	publicIP, err := checkPublicIP(ctx, cfg)
	if err != nil {
		return stageErrf(stagePublicIP, "failed to get public IP: %w", err)
	}
	log.Info().Str("public_ip", publicIP.String()).Msg("resolved public IP")

	// Reset first: without it every observed IP keeps its own series at 1
	// forever, so several IPs would report as current at once.
	publicIPInfo.Reset()
	publicIPInfo.WithLabelValues(publicIP.String()).Set(1)

	list, err := listRecords(ctx, client, cfg)
	if err != nil {
		return stageErrf(stageListRecords, "failed to list records: %w", err)
	}
	if list.Reply.Code != namesilo.ReplyCodeSuccess {
		return stageErrf(stageListRecordsReply, "%s returned code %d: %s",
			operationListRecords, list.Reply.Code, list.Reply.Detail)
	}

	existing, matches := findARecord(list.Reply.ResourceRecords, cfg.host)
	if matches > 1 {
		log.Warn().Str("host", cfg.host).Int("matches", matches).
			Msg("multiple A records matched, using the last")
	}
	if existing.RecordID == "" {
		recordUpToDate.Set(0)
		return stageErrf(stageRecordNotFound, "no %s record found for host %q in domain %q",
			recordTypeA, cfg.host, cfg.domain)
	}

	if existing.Value == publicIP.String() {
		recordUpToDate.Set(1)
		log.Info().Str("host", cfg.host).Str("value", existing.Value).
			Msg("record is up to date, skipping update")
		return nil
	}

	log.Info().Str("host", cfg.host).Str("from", existing.Value).Str("to", publicIP.String()).
		Msg("updating record")

	resp, err := updateRecord(ctx, client, cfg, existing.RecordID, publicIP.String())
	if err != nil {
		recordUpToDate.Set(0)
		return stageErrf(stageUpdateRecord, "failed to update dns record: %w", err)
	}
	if resp.Reply.Code != namesilo.ReplyCodeSuccess {
		recordUpToDate.Set(0)
		return stageErrf(stageUpdateRecordReply, "%s returned code %d: %s",
			operationUpdateRecord, resp.Reply.Code, resp.Reply.Detail)
	}

	recordUpToDate.Set(1)
	recordChangesTotal.Inc()
	lastChangeTimestamp.SetToCurrentTime()
	log.Info().Str("host", cfg.host).Str("value", publicIP.String()).Msg("record updated")
	return nil
}

// findARecord returns the last A record matching host, and how many matched.
func findARecord(records []namesilo.ResourceRecord, host string) (namesilo.ResourceRecord, int) {
	var found namesilo.ResourceRecord
	var matches int
	for _, rec := range records {
		if rec.Type == recordTypeA && rec.Host == host {
			found = rec
			matches++
		}
	}
	return found, matches
}

func checkPublicIP(ctx context.Context, cfg updateConfig) (net.IP, error) {
	start := time.Now()
	ip, err := namesilo.PublicIPCheck(ctx, cfg.httpClient, cfg.publicIPURL)
	publicIPCheckDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		publicIPChecksTotal.WithLabelValues(resultError).Inc()
		return nil, err
	}
	publicIPChecksTotal.WithLabelValues(resultSuccess).Inc()
	return ip, nil
}

func listRecords(ctx context.Context, client *namesilo.Client, cfg updateConfig) (*namesilo.DnsListRecordsResponse, error) {
	start := time.Now()
	resp, err := client.DnsListRecords(ctx, namesilo.DnsListRecordsParameters{Domain: cfg.domain})
	code := 0
	if resp != nil {
		code = resp.Reply.Code
	}
	observeNamesilo(operationListRecords, start, code, err)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func updateRecord(ctx context.Context, client *namesilo.Client, cfg updateConfig, recordID, value string) (*namesilo.DnsUpdateRecordResponse, error) {
	start := time.Now()
	resp, err := client.DnsUpdateRecord(ctx, namesilo.DnsUpdateRecordParameters{
		Domain:  cfg.domain,
		RRID:    recordID,
		RRHost:  cfg.host,
		RRValue: value,
		RRTTL:   strconv.Itoa(cfg.ttl),
	})
	code := 0
	if resp != nil {
		code = resp.Reply.Code
	}
	observeNamesilo(operationUpdateRecord, start, code, err)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// observeNamesilo records the duration and outcome of an API call. Calls that
// never produced a reply code are labelled "error" rather than a bogus 0.
func observeNamesilo(operation string, start time.Time, code int, err error) {
	namesiloRequestDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())

	replyCode := resultError
	if err == nil {
		replyCode = strconv.Itoa(code)
	}
	namesiloRequestsTotal.WithLabelValues(operation, replyCode).Inc()
}
