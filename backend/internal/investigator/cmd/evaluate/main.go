package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/transactx/backend/internal/investigator"
)

func main() {
	root := flag.String("root", ".", "repository root")
	dataset := flag.String("dataset", "backend/internal/investigator/evaluation/cases.json", "evaluation dataset")
	out := flag.String("out", "", "output artifact path; stdout when empty")
	source := flag.String("source", "", "exact tested source commit")
	evaluatedAt := flag.String("evaluated-at", "", "UTC RFC3339 evaluation time")
	dirty := flag.Bool("dirty-tree", false, "whether the evaluated source tree was dirty")
	command := flag.String("command", "", "reproduction command recorded in the artifact")
	flag.Parse()
	if *source == "" || *evaluatedAt == "" || *command == "" {
		fatal(fmt.Errorf("-source, -evaluated-at and -command are required"))
	}
	artifact, err := investigator.EvaluateInvestigator(investigator.EvaluationOptions{
		RepositoryRoot: *root, DatasetPath: *dataset, SourceCommit: *source,
		EvaluatedAt: *evaluatedAt, Command: *command, DirtyTree: *dirty,
	})
	if artifact.SchemaVersion != "" {
		if writeErr := writeArtifact(artifact, *out); writeErr != nil {
			fatal(writeErr)
		}
	}
	if err != nil {
		fatal(err)
	}
}

func writeArtifact(artifact investigator.EvaluationArtifact, out string) error {
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if out == "" {
		_, err = os.Stdout.Write(raw)
	} else {
		err = os.WriteFile(out, raw, 0o644)
	}
	return err
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
