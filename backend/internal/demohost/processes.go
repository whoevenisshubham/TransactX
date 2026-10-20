package demohost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Service struct {
	Name, Address string
	Env           []string
}
type session struct {
	ready    chan struct{}
	err      error
	active   int
	last     time.Time
	stopping bool
}

// Processes stops every database-connected process between demo visits. Keeping
// only the worker idle would leave bank/API pool health checks waking Neon.
type Processes struct {
	mu       sync.Mutex
	current  *session
	ctx      context.Context
	bin      string
	services []Service
	idle     time.Duration
	cooldown time.Time
	log      *slog.Logger
	grace    time.Duration
	wg       sync.WaitGroup
}

func NewProcesses(ctx context.Context, bin string, services []Service, idle time.Duration, logger *slog.Logger) *Processes {
	return &Processes{ctx: ctx, bin: bin, services: services, idle: idle, log: logger, grace: 6 * time.Second}
}

func (p *Processes) Acquire(ctx context.Context) (func(), error) {
	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return nil, p.ctx.Err()
	}
	s := p.current
	if s == nil {
		if time.Now().Before(p.cooldown) {
			p.mu.Unlock()
			return nil, errors.New("demo startup cooling down")
		}
		s = &session{ready: make(chan struct{}), last: time.Now()}
		p.current = s
		p.wg.Add(1)
		go p.run(s)
	}
	if s.stopping {
		p.mu.Unlock()
		return nil, errors.New("demo is stopping")
	}
	s.active++
	s.last = time.Now()
	p.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { p.mu.Lock(); s.active--; s.last = time.Now(); p.mu.Unlock() }) }
	select {
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	case <-s.ready:
		if s.err != nil {
			release()
			return nil, s.err
		}
		return release, nil
	}
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *Processes) run(s *session) {
	defer p.wg.Done()
	var children []child
	exits := make(chan string, len(p.services))
	ready := false
	defer func() {
		p.mu.Lock()
		s.stopping = true
		p.mu.Unlock()
		// Worker is last in startup order: stop it before banks. All processes
		// receive termination before waiting; a hung child is killed after grace.
		for i := len(children) - 1; i >= 0; i-- {
			_ = children[i].cmd.Process.Signal(syscall.SIGTERM)
		}
		deadline := time.After(p.grace)
		for _, c := range children {
			select {
			case <-c.done:
			case <-deadline:
				for _, remaining := range children {
					_ = remaining.cmd.Process.Kill()
				}
			}
		}
		for _, c := range children {
			<-c.done
		}
		p.mu.Lock()
		if p.current == s {
			p.current = nil
			p.cooldown = time.Now().Add(time.Second)
		}
		if !ready {
			s.err = errors.New("service group failed to become ready")
			close(s.ready)
		}
		p.mu.Unlock()
		p.log.Info("demo database-connected processes stopped")
	}()
	startup, cancel := context.WithTimeout(p.ctx, 45*time.Second)
	defer cancel()
	for _, service := range p.services {
		path := filepath.Join(p.bin, service.Name)
		cmd := exec.Command(path)
		cmd.Env = append(os.Environ(), service.Env...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			p.log.Error("start demo process", "service", service.Name)
			return
		}
		c := child{cmd: cmd, done: make(chan struct{})}
		children = append(children, c)
		go func(c child, name string) { _ = c.cmd.Wait(); close(c.done); exits <- name }(c, service.Name)
		if service.Address != "" {
			if err := waitPort(startup, service.Address, exits); err != nil {
				p.log.Error("demo startup failed", "service", service.Name)
				return
			}
		}
	}
	p.mu.Lock()
	close(s.ready)
	ready = true
	p.mu.Unlock()
	p.log.Info("demo service group ready")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case name := <-exits:
			p.log.Error("demo child exited; stopping group", "service", name)
			return
		case <-ticker.C:
			p.mu.Lock()
			idle := s.active == 0 && time.Since(s.last) >= p.idle
			if idle {
				s.stopping = true
			}
			p.mu.Unlock()
			if idle {
				return
			}
		}
	}
}

func waitPort(ctx context.Context, address string, exits <-chan string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case name := <-exits:
			return fmt.Errorf("child %s exited", name)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

func (p *Processes) Wait() { p.wg.Wait() }
