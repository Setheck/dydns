package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
	"github.com/setheck/dydns/pkg/namesilo"
)

type environment struct {
	NamesiloAPIKey         string        `envconfig:"NAMESILO_API_KEY" required:"true"`
	NamesiloDomain         string        `envconfig:"NAMESILO_DOMAIN" required:"true"`
	NamesiloHost           string        `envconfig:"NAMESILO_HOST" required:"true"`
	NamesiloUpdateInterval time.Duration `envconfig:"NAMESILO_UPDATE_INTERVAL" default:"24h"`
}

var log = zerolog.New(zerolog.ConsoleWriter{
	Out:        os.Stdout,
	TimeFormat: time.RFC3339}).With().Timestamp().Logger()

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	env := &environment{}
	if err := envconfig.Process("", env); err != nil {
		log.Fatal().Err(err).Msg("failed to process environment")
	}

	log.Info().Msg("DYDNS")
	log.Info().Str("HOST", env.NamesiloHost).
		Str("DOMAIN", env.NamesiloDomain).
		Str("UPDATE_INTERVAL", env.NamesiloUpdateInterval.String()).
		Msg("starting up")

	updateOnInterval(ctx, updateConfig{
		apiKey:   env.NamesiloAPIKey,
		domain:   env.NamesiloDomain,
		host:     env.NamesiloHost,
		interval: env.NamesiloUpdateInterval,
	})
}

type updateConfig struct {
	apiKey   string
	domain   string
	host     string
	interval time.Duration
}

func updateOnInterval(ctx context.Context, cfg updateConfig) {
	client := namesilo.New(cfg.apiKey)
	for {

		log.Info().Msg("-- updating dynamic DNS --")
		updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := updateDynamicDNS(updateCtx, client, cfg); err != nil {
			log.Error().Err(err).Msg("failed")
		}
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.interval):
		}
	}
}

func updateDynamicDNS(ctx context.Context, client *namesilo.Client, cfg updateConfig) error {
	publicIP, err := namesilo.PublicIPCheck()
	if err != nil {
		return fmt.Errorf("failed to get public IP: %w", err)
	}
	log.Info().Msgf("public IP: %s", publicIP)

	list, err := client.DnsListRecords(ctx, namesilo.DnsListRecordsParameters{Domain: cfg.domain})
	if err != nil {
		return fmt.Errorf("failed to list records: %w", err)
	}

	if list.Reply.Code != 300 {
		return fmt.Errorf("invalid code: %d", list.Reply.Code)
	}

	var existingRecord namesilo.ResourceRecord
	targetFqdnHost := fmt.Sprintf("%s", cfg.host)
	for _, rec := range list.Reply.ResourceRecords {
		if rec.Type == "A" && rec.Host == cfg.host {
			existingRecord = rec
		}

	}
	if existingRecord.RecordID == "" {
		return fmt.Errorf("failed to find fqdn: %s, record_id: %s", targetFqdnHost, existingRecord.RecordID)
	}
	if existingRecord.Value == publicIP.String() {
		log.Info().Msgf("record %s is up to date, skipping update", targetFqdnHost)
		return nil
	}

	resp, err := client.DnsUpdateRecord(ctx, namesilo.DnsUpdateRecordParameters{
		Domain:  cfg.domain,
		RRID:    existingRecord.RecordID,
		RRHost:  cfg.host,
		RRValue: publicIP.String(),
		RRTTL:   "7207",
	})
	if err != nil {
		return fmt.Errorf("failed to update dns record: %w", err)
	}
	if resp.Reply.Code != 300 {
		return fmt.Errorf("invalid code: %d - details: %s", resp.Reply.Code, resp.Reply.Detail)
	}
	return nil
}
