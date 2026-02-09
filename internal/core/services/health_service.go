package services

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
)

// ComponentHealth represents the health of a specific component
type ComponentHealth struct {
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Latency   string       `json:"latency,omitempty"`
	CheckedAt time.Time    `json:"checked_at"`
}

// HealthReport represents the overall health report
type HealthReport struct {
	Status     HealthStatus               `json:"status"`
	Version    string                     `json:"version"`
	CheckedAt  time.Time                  `json:"checked_at"`
	Components map[string]ComponentHealth `json:"components"`
}

type HealthService struct {
	db     *gorm.DB
	redis  *redis.Client
	version string
}

func NewHealthService(db *gorm.DB, redisClient *redis.Client, version string) *HealthService {
	if version == "" {
		version = "0.0.1"
	}
	return &HealthService{
		db:      db,
		redis:   redisClient,
		version: version,
	}
}

func (s *HealthService) CheckHealth(ctx context.Context) *HealthReport {
	report := &HealthReport{
		Status:     HealthStatusHealthy,
		Version:    s.version,
		CheckedAt:  time.Now(),
		Components: make(map[string]ComponentHealth),
	}

	// Check Database
	dbHealth := s.checkDatabase(ctx)
	report.Components["database"] = dbHealth
	if dbHealth.Status != HealthStatusHealthy {
		report.Status = HealthStatusUnhealthy
	}

	// Check Redis
	redisHealth := s.checkRedis(ctx)
	report.Components["redis"] = redisHealth
	if redisHealth.Status != HealthStatusHealthy && report.Status == HealthStatusHealthy {
		report.Status = HealthStatusDegraded
	}

	return report
}

func (s *HealthService) checkDatabase(ctx context.Context) ComponentHealth {
	start := time.Now()
	
	sqlDB, err := s.db.DB()
	if err != nil {
		return ComponentHealth{
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Failed to get database instance: %v", err),
			CheckedAt: time.Now(),
		}
	}

	// Check connection with timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return ComponentHealth{
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Database ping failed: %v", err),
			Latency:   time.Since(start).String(),
			CheckedAt: time.Now(),
		}
	}

	// Check if we can execute a simple query
	var result int
	if err := s.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return ComponentHealth{
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Database query failed: %v", err),
			Latency:   time.Since(start).String(),
			CheckedAt: time.Now(),
		}
	}

	return ComponentHealth{
		Status:    HealthStatusHealthy,
		Latency:   time.Since(start).String(),
		CheckedAt: time.Now(),
	}
}

func (s *HealthService) checkRedis(ctx context.Context) ComponentHealth {
	start := time.Now()

	if s.redis == nil {
		return ComponentHealth{
			Status:    HealthStatusUnhealthy,
			Message:   "Redis client not initialized",
			CheckedAt: time.Now(),
		}
	}

	// Check connection with timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.redis.Ping(ctx).Err(); err != nil {
		return ComponentHealth{
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Redis ping failed: %v", err),
			Latency:   time.Since(start).String(),
			CheckedAt: time.Now(),
		}
	}

	return ComponentHealth{
		Status:    HealthStatusHealthy,
		Latency:   time.Since(start).String(),
		CheckedAt: time.Now(),
	}
}

// SimpleHealthCheck returns a simple health status for load balancers
func (s *HealthService) SimpleHealthCheck(ctx context.Context) (string, int) {
	report := s.CheckHealth(ctx)
	
	switch report.Status {
	case HealthStatusHealthy:
		return "ok", 200
	case HealthStatusDegraded:
		return "degraded", 200 // Still serving requests
	default:
		return "unhealthy", 503
	}
}
