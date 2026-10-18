// Package demoenrich is an explicit synthetic-demo client, never a ledger writer.
package demoenrich

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const ManifestVersion = "experience-v3-enrichment-v1"

type Config struct {
	DatabaseURL, ExpectedDatabase, ExpectedHost, ExpectedProject, APIOrigin, SourceSHA, Ack, ReceiptFile string
	Apply                                                                                                bool
}
type Entry struct {
	Number      int    `json:"number"`
	Sender      string `json:"sender"`
	Persona     string `json:"persona"`
	Recipient   string `json:"recipient"`
	AmountPaise int64  `json:"amountPaise"`
	Key         string `json:"idempotencyKey"`
	PaymentID   string `json:"paymentId,omitempty"`
	State       string `json:"state,omitempty"`
}

func (c Config) Validate() error {
	db, err := url.Parse(c.DatabaseURL)
	if err != nil || db == nil || db.User == nil || !strings.HasSuffix(db.Hostname(), ".neon.tech") || strings.Contains(db.Hostname(), "-pooler") || db.Hostname() != c.ExpectedHost || strings.TrimPrefix(db.Path, "/") != c.ExpectedDatabase || (db.Query().Get("sslmode") != "require" && db.Query().Get("sslmode") != "verify-full") {
		return errors.New("requires the exact acknowledged direct Neon demo endpoint/database with TLS")
	}
	if !strings.Contains(strings.ToLower(c.ExpectedDatabase), "demo") || c.ExpectedDatabase == "postgres" || c.ExpectedProject == "" {
		return errors.New("protected/default/local source databases are not enrichment targets")
	}
	origin, err := url.Parse(c.APIOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Path != "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return errors.New("requires an exact real HTTPS API origin")
	}
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(c.SourceSHA) {
		return errors.New("record the full reviewed tool source SHA")
	}
	if c.Apply && c.Ack != "ENRICH SYNTHETIC DEMO "+c.ExpectedProject+"/"+c.ExpectedDatabase {
		return errors.New("apply requires the exact synthetic project/database acknowledgement")
	}
	return nil
}
func Plan(c Config) []Entry {
	routes := [][3]string{{"alice@transactx", "customer", "bob@transactx"}, {"bob@transactx", "recipient", "store@transactx"}, {"alice@transactx", "customer", "store@transactx"}}
	entries := make([]Entry, 12)
	for i := range entries {
		route := routes[i%len(routes)]
		entries[i] = Entry{Number: i + 1, Sender: route[0], Persona: route[1], Recipient: route[2], AmountPaise: int64(100 + i*5), Key: uuid.NewSHA1(uuid.NameSpaceURL, []byte(c.ExpectedProject+"/"+c.ExpectedDatabase+"/"+ManifestVersion+"/"+string(rune('A'+i)))).String()}
	}
	return entries
}

// These are the existing demo initializer's enforced limits, not invented quota.
func Available(today, total int64) int64 {
	daily := int64(50) - today
	lifetime := int64(1000) - total
	if lifetime < daily {
		return lifetime
	}
	return daily
}
func CanEnrich(today, total, missing int64) bool {
	return today >= 0 && total >= 0 && missing >= 0 && Available(today, total)-missing >= 10
}
