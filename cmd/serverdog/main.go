// Command serverdog serves servers.dog. Wiring: config → source → tracker → scheduler → publish → http (SPEC §10.2).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ogredforman/ServerDog/internal/clock"
	"github.com/ogredforman/ServerDog/internal/config"
	"github.com/ogredforman/ServerDog/internal/publish"
	"github.com/ogredforman/ServerDog/internal/scheduler"
	"github.com/ogredforman/ServerDog/internal/source"
	"github.com/ogredforman/ServerDog/internal/source/fake"
	"github.com/ogredforman/ServerDog/internal/tracker"
	"github.com/ogredforman/ServerDog/internal/web"
)

// fakeSeed fixes the generated world for source=fake, so local runs are reproducible.
const fakeSeed = 1

func main() {
	if err := run(); err != nil {
		slog.Error("serverdog stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	clk := clock.Real{}

	src, err := newSource(cfg, clk)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	site := web.New(clk, cfg.DataDir)
	trk := tracker.New(tracker.Options{
		StartingWindow: cfg.StartingWindow,
		DropPct:        cfg.DropPct,
		DropMin:        cfg.DropMin,
	})
	sched, err := scheduler.New(src, clk, trk, scheduler.Options{
		BurstRate:   cfg.BurstRate,
		BurstBudget: cfg.BurstBudget,
		SweepRate:   cfg.SweepRate,
		Concurrency: cfg.Concurrency,
		PollDelay:   cfg.PollDelay,
		Publish: func(ctx context.Context, f publish.File) error {
			if err := publish.Write(ctx, cfg.DataDir, f); err != nil {
				return err
			}
			site.Published(f)
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("create scheduler: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	schedCtx, cancelSched := context.WithCancel(ctx)
	defer cancelSched()
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		sched.Run(schedCtx)
	}()

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           site,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen, "source", cfg.Source, "data_dir", cfg.DataDir)
		errc <- srv.ListenAndServe()
	}()

	var serveErr error
	select {
	case serveErr = <-errc:
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	cancelSched()
	<-schedDone
	if serveErr != nil {
		return serveErr
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newSource builds the configured Source. This is the only non-source package that imports a
// provider package (SPEC §10.3).
func newSource(cfg config.Config, clk clock.Clock) (source.Source, error) {
	switch cfg.Source {
	case config.SourceFake:
		return fake.NewWorld(clk, fakeSeed), nil
	default:
		return nil, fmt.Errorf("source %q is not available until PLAN 2.1; run with --source fake", cfg.Source)
	}
}
