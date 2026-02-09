package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	grpc_handler "picpic.render/internal/adapters/handler/grpc"
	http_handler "picpic.render/internal/adapters/handler/http"
	redis_adapter "picpic.render/internal/adapters/queue/redis"
	"picpic.render/internal/adapters/repository/pg"
	"picpic.render/internal/config"
	"picpic.render/internal/core/services"
	"picpic.render/proto"
)

func main() {
	cfg := config.Load()

	// Initialize adapters
	jobRepo, agentRepo, err := pg.NewRepository(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to init postgres: %v", err)
	}

	queue, pubsub, redisClient, err := redis_adapter.NewRedisAdapter(cfg.RedisURL)
	if err != nil {
		log.Fatalf("failed to init redis: %v", err)
	}

	// Initialize domain services
	jobService := services.NewJobService(jobRepo, agentRepo, queue, pubsub)

	// Get DB instance for health check
	pgRepo := jobRepo.(*pg.Repository)
	db, _ := pgRepo.DB()
	healthService := services.NewHealthService(db, redisClient, "0.1.0")

	// Initialize gRPC server first (needed by HTTP server)
	grpcServer := grpc_handler.NewServer(jobService)

	// Initialize HTTP handlers
	hub := http_handler.NewHub(pubsub)
	go hub.Run()
	go hub.LogConsumer(context.Background())

	httpServer := http_handler.NewServer(jobService, healthService, hub, grpcServer)

	// Start HTTP Server
	go func() {
		log.Printf("HTTP Server listening on port 8080")
		if err := httpServer.Run(":8080"); err != nil {
			log.Fatalf("failed to serve http: %v", err)
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	proto.RegisterWoodpeckerServer(s, grpcServer)
	proto.RegisterWoodpeckerAuthServer(s, grpcServer)

	log.Printf("gRPC Server listening on port %s", cfg.Port)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
