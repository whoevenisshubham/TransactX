package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func privateOutput(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false
	}
	for p := parent; ; p = filepath.Dir(p) {
		if filepath.Base(p) == ".git" {
			return false
		}
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}

// Encrypt the bounded exact response immediately to an exclusive private file.
// No plaintext is written to disk/stdout/stderr and no identity/key is passed
// to the provider. Preserve ciphertext even after validation fails, so one
// authorized request supports unlimited OFFLINE diagnosis instead of more calls.
func prepareResponseCapture(path, recipient string) (func([]byte) error, func(), error) {
	if !privateOutput(path) || !filepath.IsAbs(recipient) {
		return nil, nil, errors.New("private encrypted response path and existing age recipient file required")
	}
	age, err := exec.LookPath("age")
	if err != nil {
		return nil, nil, errors.New("installed age encryption tool required before provider admission")
	}
	encrypt := func(raw []byte, destination io.Writer) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, age, "--recipients-file", recipient)
		command.Stdin = bytes.NewReader(raw)
		command.Stdout = destination
		command.Stderr = io.Discard
		if command.Run() != nil {
			return errors.New("private response encryption failed")
		}
		return nil
	}
	// Validate the existing recipient/tool before any provider admission.
	if err = encrypt(nil, io.Discard); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, errors.New("encrypted capture exists or cannot be created; no automatic retry")
	}
	used := false
	capture := func(raw []byte) error {
		if used || len(raw) > 1<<20 {
			return errors.New("one bounded response capture required")
		}
		used = true
		if err := encrypt(raw, f); err != nil {
			return err
		}
		return f.Sync()
	}
	return capture, func() { f.Close() }, nil
}
