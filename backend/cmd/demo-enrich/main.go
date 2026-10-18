package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/transactx/backend/internal/demoenrich"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"
)

func main() {
	var c demoenrich.Config
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	flag.StringVar(&c.ExpectedDatabase, "expected-db", "", "separate synthetic demo database")
	flag.StringVar(&c.ExpectedHost, "expected-host", "", "exact direct Neon endpoint hostname")
	flag.StringVar(&c.ExpectedProject, "expected-project", "", "owner-verified Neon project ID")
	flag.StringVar(&c.APIOrigin, "api-origin", "", "actual authorized HTTPS API origin")
	flag.StringVar(&c.SourceSHA, "source-sha", "", "full reviewed tool source SHA (not a claim of deployed backend SHA)")
	flag.BoolVar(&c.Apply, "apply", false, "perform acknowledged real API payments; default read-only dry-run")
	flag.StringVar(&c.Ack, "ack", "", "exact project/database synthetic-enrichment acknowledgement")
	flag.StringVar(&c.ReceiptFile, "receipt-file", "", "absolute private append-only evidence path")
	flag.Parse()
	actualSHA := ""
	dirty := false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				actualSHA = setting.Value
			}
			if setting.Key == "vcs.modified" {
				dirty = setting.Value == "true"
			}
		}
	}
	if actualSHA == "" {
		head, err := exec.Command("git", "rev-parse", "HEAD").Output()
		status, statusErr := exec.Command("git", "status", "--porcelain").Output()
		if err != nil || statusErr != nil {
			fmt.Fprintln(os.Stderr, "Build from the reviewed clean Git source with VCS metadata")
			os.Exit(1)
		}
		actualSHA = strings.TrimSpace(string(head))
		dirty = len(strings.TrimSpace(string(status))) > 0
	}
	if actualSHA != c.SourceSHA || (c.Apply && dirty) {
		fmt.Fprintln(os.Stderr, "Tool source SHA must match; apply requires reviewed clean source")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := demoenrich.Run(ctx, c, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
