package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"picpic.render/internal/agent"
)

func main() {
	serverAddr := os.Getenv("AGENT_SERVER")
	if serverAddr == "" {
		serverAddr = "localhost:9000"
	}

	secret := os.Getenv("AGENT_SECRET")
	if secret == "" {
		log.Fatal("AGENT_SECRET environment variable is required")
	}

	capacityStr := os.Getenv("AGENT_CAPACITY")
	capacity := 1
	if capacityStr != "" {
		if c, err := strconv.Atoi(capacityStr); err == nil && c > 0 {
			capacity = c
		}
	}

	log.Printf("Starting Custom Agent")
	log.Printf("Server: %s", serverAddr)
	log.Printf("Capacity: %d", capacity)

	a, err := agent.New(serverAddr, secret, capacity)
	if err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down agent...")
		cancel()
	}()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("Agent error: %v", err)
	}
}
