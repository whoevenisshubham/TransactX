package demohost

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// An actual child OS process with a TCP listener, but no database or secrets.
func TestMain(m *testing.M) {
	if os.Getenv("TX_DEMO_PROCESS_HELPER") == "1" {
		listener, err := net.Listen("tcp", os.Getenv("TX_HELPER_ADDRESS"))
		if err != nil {
			os.Exit(2)
		}
		defer listener.Close()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if delay := os.Getenv("TX_HELPER_EXIT_AFTER"); delay != "" {
			d, _ := time.ParseDuration(delay)
			go func() { time.Sleep(d); cancel() }()
		}
		go func() {
			for {
				c, e := listener.Accept()
				if e != nil {
					return
				}
				_ = c.Close()
			}
		}()
		<-ctx.Done()
		return
	}
	os.Exit(m.Run())
}

func helperService(t *testing.T, bin, name, exit string) Service {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(bin, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal("copy helper failed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return Service{Name: name, Address: address, Env: []string{"TX_DEMO_PROCESS_HELPER=1", "TX_HELPER_ADDRESS=" + address, "TX_HELPER_EXIT_AFTER=" + exit}}
}

func TestNativeChildGroupStopsOnCancellation(t *testing.T) {
	bin := t.TempDir()
	services := []Service{helperService(t, bin, "bank", ""), helperService(t, bin, "api", "")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewProcesses(ctx, bin, services, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.grace = 50 * time.Millisecond
	request, finish := context.WithTimeout(ctx, 5*time.Second)
	defer finish()
	release, err := p.Acquire(request)
	if err != nil {
		t.Fatal(err)
	}
	release()
	cancel()
	p.Wait()
	for _, s := range services {
		c, err := net.DialTimeout("tcp", s.Address, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			t.Fatal("child listener survived group cancellation")
		}
	}
}

func TestNativeChildFailureStopsPeers(t *testing.T) {
	bin := t.TempDir()
	services := []Service{helperService(t, bin, "bank", ""), helperService(t, bin, "api", "500ms")}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	p := NewProcesses(ctx, bin, services, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.grace = 50 * time.Millisecond
	release, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	p.Wait()
	if ctx.Err() != nil {
		t.Fatal("child failure did not stop group promptly")
	}
	c, err := net.DialTimeout("tcp", services[0].Address, 100*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("peer survived child failure")
	}
}
