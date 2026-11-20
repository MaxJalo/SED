package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.Println("sed-worker scaffold started (SLA & Outbox worker)")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("sed-worker heartbeat (Stage 0 scaffold)")
		case sig := <-sigCh:
			log.Printf("sed-worker shutting down on signal %v", sig)
			return
		}
	}
}
