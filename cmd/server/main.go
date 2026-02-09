package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	grpc_handler "picpic.render/internal/adapters/handler/grpc"
	http_handler "picpic.render/internal/adapters/handler/http"
	"picpic.render/internal/adapters/handler/mqtt"
	redis_adapter "picpic.render/internal/adapters/queue/redis"
	"picpic.render/internal/adapters/repository/pg"
	"picpic.render/internal/config"
	"picpic.render/internal/core/logger"
	"picpic.render/internal/core/services"
	"picpic.render/internal/core/tracing"
	"picpic.render/proto"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize structured logger
	logger.Init(cfg.LogLevel, cfg.LogFormat)
	logger.Info("Starting PicPic Render Server", "version", "0.1.0")

	// Initialize tracing
	var shutdownTracing func(context.Context) error
	if cfg.EnableTracing {
		shutdownTracing, err = tracing.Init(cfg.ServiceName, cfg.OTLPEndpoint)
		if err != nil {
			logger.Error("Failed to initialize tracing", "error", err)
		} else {
			logger.Info("Tracing initialized", "endpoint", cfg.OTLPEndpoint)
			defer func() {
				if err := shutdownTracing(context.Background()); err != nil {
					logger.Error("Failed to shutdown tracing", "error", err)
				}
			}()
		}
	}

	// Initialize adapters
	jobRepo, agentRepo, err := pg.NewRepository(cfg.DatabaseURL)
	if err != nil {
		logger.Error("Failed to init postgres", "error", err)
		log.Fatalf("failed to init postgres: %v", err)
	}

	queue, pubsub, redisClient, err := redis_adapter.NewRedisAdapter(cfg.RedisURL)
	if err != nil {
		logger.Error("Failed to init redis", "error", err)
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
	// Initialize HTTP handlers
	hub := http_handler.NewHub(pubsub)
	go hub.Run()

	// Initialize MQTT Publisher
	mqttPublisher, err := mqtt.NewPublisher(pubsub, "tcp://broker.mqtt-dashboard.com:1883")
	if err != nil {
		logger.Error("Failed to init MQTT publisher", "error", err)
	} else {
		mqttPublisher.Start(context.Background())
		logger.Info("MQTT Publisher started")
	}

	// Consumers disabled for Hub, using MQTT instead
	// go hub.LogConsumer(context.Background())
	// go hub.ResourceConsumer(context.Background())
	// go hub.JobUpdateConsumer(context.Background())

	httpServer := http_handler.NewServer(jobService, healthService, hub, grpcServer)

	// Start HTTP Server
	go func() {
		logger.Info("HTTP Server starting", "port", cfg.HTTPPort)
		if err := httpServer.Run(":" + cfg.HTTPPort); err != nil {
			logger.Error("HTTP server failed", "error", err)
			log.Fatalf("failed to serve http: %v", err)
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.Port))
	if err != nil {
		logger.Error("Failed to listen", "error", err, "port", cfg.Port)
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	proto.RegisterWoodpeckerServer(s, grpcServer)
	proto.RegisterWoodpeckerAuthServer(s, grpcServer)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutting down gracefully...")
		s.GracefulStop()
		if shutdownTracing != nil {
			shutdownTracing(context.Background())
		}
		os.Exit(0)
	}()

	logger.Info("gRPC Server starting", "port", cfg.Port)
	if err := s.Serve(lis); err != nil {
		logger.Error("gRPC server failed", "error", err)
		log.Fatalf("failed to serve: %v", err)
	}
}
