// Command maintain-merkle is the explicit maintenance boundary for durable
// canonical and participant Merkle commitments. API reads never invoke it.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/reconciliation"
)

type maintainedView struct {
	Owner            string `json:"owner"`
	ParticipantID    string `json:"participantId"`
	ScopeFrom        string `json:"scopeFrom"`
	ScopeTo          string `json:"scopeTo"`
	Generation       string `json:"generation"`
	RootHex          string `json:"rootHex"`
	RecordCount      int64  `json:"recordCount"`
	CanonicalVersion string `json:"canonicalVersion"`
	AlgorithmVersion string `json:"algorithmVersion"`
}

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
		participant = flag.String("participant", "", "participant bank code")
		bankURL     = flag.String("bank-url", "", "participant service URL")
		fromRaw     = flag.String("scope-from", "", "closed scope start (RFC3339)")
		toRaw       = flag.String("scope-to", "", "closed scope end (RFC3339)")
		bucketWidth = flag.Duration("bucket-width", 15*time.Minute, "Merkle bucket width")
	)
	flag.Parse()

	if *databaseURL == "" || *participant == "" || *bankURL == "" || *fromRaw == "" || *toRaw == "" {
		exitf("database-url, participant, bank-url, scope-from, and scope-to are required")
	}
	from, err := time.Parse(time.RFC3339Nano, *fromRaw)
	if err != nil {
		exitf("parse scope-from: %v", err)
	}
	to, err := time.Parse(time.RFC3339Nano, *toRaw)
	if err != nil {
		exitf("parse scope-to: %v", err)
	}
	scope := reconciliation.Scope{From: from.UTC(), To: to.UTC()}
	if err := scope.Validate(); err != nil || from.IsZero() || to.IsZero() {
		exitf("invalid closed scope: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := database.NewPool(ctx, *databaseURL)
	if err != nil {
		exitf("connect to PostgreSQL: %v", err)
	}
	defer db.Close()
	adapter, err := bank.NewHTTPClient(*bankURL, nil)
	if err != nil {
		exitf("configure participant adapter: %v", err)
	}

	participantID := strings.TrimSpace(*participant)
	canonicalSource := reconciliation.NewProjectedCentralLedgerSnapshotSource(db, participantID, adapter)
	canonical, err := reconciliation.NewDurableRepositoryParticipant(db, canonicalSource, participantID, "canonical:"+participantID, *bucketWidth)
	if err != nil {
		exitf("construct canonical participant: %v", err)
	}
	remote, err := reconciliation.NewDurableRepositoryParticipant(db, adapter, participantID, "participant:"+participantID, *bucketWidth)
	if err != nil {
		exitf("construct participant: %v", err)
	}
	views := []struct {
		owner string
		view  *reconciliation.RepositoryParticipant
	}{
		{owner: "canonical:" + participantID, view: canonical},
		{owner: "participant:" + participantID, view: remote},
	}
	result := make([]maintainedView, 0, len(views))
	for _, item := range views {
		if err := item.view.Initialize(ctx, scope); err != nil {
			exitf("maintain %s: %v", item.owner, err)
		}
		root, err := item.view.GetRoot(ctx, scope)
		if err != nil {
			exitf("load %s: %v", item.owner, err)
		}
		metadata, err := item.view.GetMetadata(ctx, scope)
		if err != nil {
			exitf("metadata %s: %v", item.owner, err)
		}
		result = append(result, maintainedView{
			Owner: item.owner, ParticipantID: metadata.ParticipantID,
			ScopeFrom: scope.From.Format(time.RFC3339Nano), ScopeTo: scope.To.Format(time.RFC3339Nano),
			Generation: root.Ref.Generation, RootHex: hex.EncodeToString(root.Root), RecordCount: metadata.RecordCount,
			CanonicalVersion: root.Version, AlgorithmVersion: root.Algorithm,
		})
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		exitf("encode result: %v", err)
	}
}

func exitf(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
