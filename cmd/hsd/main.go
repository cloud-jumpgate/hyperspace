// hsd is the Hyperspace driver daemon.
// Usage: hsd [--threading dedicated|dense|shared]
//
// In this sprint, hsd starts the embedded driver, logs to stdout, and runs until SIGINT/SIGTERM.
package main

import (
	"context"
	"flag"
	"log"
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
		log.Fatalf("failed to create driver: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		log.Fatalf("failed to start driver: %v", err)
	}

	log.Printf("hsd started (threading=%s)", *threadingStr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down hsd...")
	d.Stop()
	log.Println("hsd stopped")
}
