package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/setheck/dydns/pkg/namesilo"
)

type environment struct {
	NamesiloAPIKey string `envconfig:"NAMESILO_API_KEY" required:"true"`
	NamesiloDomain string `envconfig:"NAMESILO_DOMAIN" required:"true"`
	NamesiloHost   string `envconfig:"NAMESILO_HOST" required:"true"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	env := &environment{}
	if err := envconfig.Process("", env); err != nil {
		log.Fatal(err)
	}

	log.Println("DYDNS starting up.")
	log.Println("HOST:", env.NamesiloHost)
	log.Println("DOMAIN:", env.NamesiloDomain)

	updateOnInterval(ctx, updateConfig{
		apiKey:   env.NamesiloAPIKey,
		domain:   env.NamesiloDomain,
		host:     env.NamesiloHost,
		interval: 5 * time.Minute,
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
		updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := updateDynamicDNS(updateCtx, client, cfg); err != nil {
			log.Println("failed", err)
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
	log.Println("updating dynamic DNS")
	publicIP, err := namesilo.PublicIPCheck()
	if err != nil {
		return fmt.Errorf("failed to get public IP: %w", err)
	}

	list, err := client.DnsListRecords(ctx, namesilo.DnsListRecordsParameters{Domain: cfg.domain})
	if err != nil {
		return fmt.Errorf("failed to list records: %w", err)
	}

	if list.Reply.Code != 300 {
		return fmt.Errorf("invalid code: %d", list.Reply.Code)
	}

	recordID := ""
	targetFqdnHost := fmt.Sprintf("%s.%s", cfg.host, cfg.domain)
	for _, rec := range list.Reply.ResourceRecords {
		if rec.Host == targetFqdnHost {
			recordID = rec.RecordID
		}
	}
	if recordID == "" {
		return fmt.Errorf("failed to find fqdn: %s, record_id: %s", targetFqdnHost, recordID)
	}

	resp, err := client.DnsUpdateRecord(ctx, namesilo.DnsUpdateRecordParameters{
		Domain:  cfg.domain,
		RRID:    recordID,
		RRHost:  cfg.host,
		RRValue: publicIP.String(),
		RRTTL:   "7207",
	})
	if err != nil {
		return fmt.Errorf("failed to update dns record: %w", err)
	}

	log.Println("Update Successful")
	log.Println("Code:", resp.Reply.Code)
	log.Println("Detail:", resp.Reply.Detail)
	return nil
}
