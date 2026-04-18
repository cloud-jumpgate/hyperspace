// hsd is the Hyperspace driver daemon.
// Usage: hsd [--threading dedicated|dense|shared]
//
// In this sprint, hsd starts the embedded driver, logs to stdout, and runs until SIGINT/SIGTERM.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloud-jumpgate/hyperspace/pkg/driver"
)

func main() {
	threadingStr := flag.String("threading", "dedicated", "threading mode: dedicated|dense|shared")
	flag.Parse()

	mode := driver.ThreadingModeDedicated
	switch *threadingStr {
	case "dense":
		mode = driver.ThreadingModeDense
	case "shared":
		mode = driver.ThreadingModeShared
	}

	cfg := driver.DefaultConfig()
	cfg.Threading = mode

	d, err := driver.NewEmbedded(cfg)
	if err != nil {
		slog.Error("failed to create driver", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		slog.Error("failed to start driver", "error", err)
		os.Exit(1)
	}

	slog.Info("hsd started", "threading", *threadingStr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down hsd...")
	d.Stop()
	slog.Info("hsd stopped")
}
