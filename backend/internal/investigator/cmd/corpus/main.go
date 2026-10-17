package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/investigator"
)

func main() {
	var (
		root        = flag.String("root", ".", "repository root")
		manifest    = flag.String("manifest", "docs/runbooks/corpus-manifest.json", "approved corpus manifest")
		databaseURL = flag.String("database-url", "", "PostgreSQL URL; required unless -dry-run")
		dryRun      = flag.Bool("dry-run", false, "validate and chunk without database writes")
		activate    = flag.Bool("activate", false, "atomically activate the staged version")
	)
	flag.Parse()

	build, err := investigator.BuildCorpus(*root, *manifest)
	if err != nil {
		fatal(err)
	}
	if !*dryRun {
		if *databaseURL == "" {
			fatal(fmt.Errorf("-database-url is required unless -dry-run is set"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, *databaseURL)
		if err != nil {
			fatal(err)
		}
		defer pool.Close()
		repository, err := investigator.NewPostgresCorpusRepository(pool)
		if err != nil {
			fatal(err)
		}
		if err := repository.Stage(ctx, build); err != nil {
			fatal(err)
		}
		if *activate {
			if err := repository.Activate(ctx, build.Version, build.ManifestSHA256); err != nil {
				fatal(err)
			}
		}
	}

	result := struct {
		Version        string `json:"version"`
		ManifestSHA256 string `json:"manifestSha256"`
		ChunkCount     int    `json:"chunkCount"`
		DryRun         bool   `json:"dryRun"`
		Activated      bool   `json:"activated"`
	}{build.Version, build.ManifestSHA256, len(build.Chunks), *dryRun, !*dryRun && *activate}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
