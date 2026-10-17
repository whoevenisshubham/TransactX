package experiments

import (
	"os/exec"
	"strings"
)

// Unknown provenance is never reported as clean. dir is empty for the runner's
// current checkout, or an isolated repository in the regression test.
func sourceProvenance(dir string) (commit, workingTree string, dirty bool) {
	commit, workingTree, dirty = "unknown", "unknown", true
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		return cmd.Output()
	}
	out, err := run("rev-parse", "HEAD")
	if err != nil {
		return
	}
	commit = strings.TrimSpace(string(out))
	out, err = run("status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return
	}
	dirty = len(out) != 0
	workingTree = "clean"
	if dirty {
		workingTree = "dirty"
	}
	return
}
