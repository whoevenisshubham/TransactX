package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedCaptureFailureStopsResponseWithoutRetry(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "receipt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	calls := 0
	a := auditTransport{receipt: f, capture: func([]byte) error { return errors.New("fixture encryption unavailable") }, next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	r, _ := http.NewRequest("POST", "https://generativelanguage.googleapis.com/v1beta/models/fixture:generateContent", nil)
	if _, err = a.RoundTrip(r); err == nil || calls != 1 {
		t.Fatal("capture failure did not stop the single attempt")
	}
}

func TestDiagnosticCaptureRejectsRepositoryAndRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if privateOutput(filepath.Join(root, "response.age")) || privateOutput("relative.age") {
		t.Fatal("unsafe output accepted")
	}
	if _, _, err := prepareResponseCapture("relative.age", "relative.recipient"); err == nil {
		t.Fatal("invalid capture configuration accepted")
	}
}
