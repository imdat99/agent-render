package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	grpcHandler "picpic.render/internal/adapters/handler/grpc"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/services"
)

type Server struct {
	router     *chi.Mux
	jobService *services.JobService
	grpcServer *grpcHandler.Server
	healthSvc  *services.HealthService
	hub        *Hub
}

func NewServer(jobService *services.JobService, healthSvc *services.HealthService, hub *Hub, grpcServer *grpcHandler.Server) *Server {
	s := &Server{
		router:     chi.NewRouter(),
		jobService: jobService,
		grpcServer: grpcServer,
		healthSvc:  healthSvc,
		hub:        hub,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(MetricsMiddleware) // Add metrics middleware
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Metrics endpoint
	s.router.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		MetricsHandler().ServeHTTP(w, r)
	})

	// Kubernetes probes
	s.router.Get("/health/live", s.handleLiveness)
	s.router.Get("/health/ready", s.handleReadiness)

	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/api/health/detailed", s.handleDetailedHealth)
	s.router.Get("/api/ws", s.handleWS)
	s.router.Get("/api/health/detailed", s.handleDetailedHealth)
	s.router.Get("/api/ws", s.handleWS)

	s.router.Route("/api/agents", func(r chi.Router) {
		r.Get("/", s.handleListAgents)
		r.Post("/{id}/restart", s.handleRestartAgent)
		r.Post("/{id}/update", s.handleUpdateAgent)
	})

	s.router.Route("/api/jobs", func(r chi.Router) {
		r.Post("/", s.handleCreateJob)
		r.Get("/", s.handleListJobs)
		r.Get("/{id}", s.handleGetJob)
		r.Get("/{id}/logs", s.handleGetJobLogs)
		r.Post("/{id}/cancel", s.handleCancelJob)
		r.Post("/{id}/retry", s.handleRetryJob)
	})

	// Serve static files for frontend (simple fs server for now)
	fileServer := http.FileServer(http.Dir("./web"))
	s.router.Handle("/*", fileServer)
}

func (s *Server) Run(addr string) error {
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, code := s.healthSvc.SimpleHealthCheck(r.Context())
	w.WriteHeader(code)
	w.Write([]byte(status))
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	// Liveness probe - just check if server is running
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Readiness probe - check if server can handle requests
	status, code := s.healthSvc.SimpleHealthCheck(r.Context())
	w.WriteHeader(code)
	w.Write([]byte(status))
}

func (s *Server) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	report := s.healthSvc.CheckHealth(r.Context())

	// Set appropriate status code based on health status
	statusCode := http.StatusOK
	switch report.Status {
	case services.HealthStatusUnhealthy:
		statusCode = http.StatusServiceUnavailable
	case services.HealthStatusDegraded:
		statusCode = http.StatusOK // Still serving requests
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(report)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ServeWs(s.hub, w, r)
}

type CreateJobRequest struct {
	Command   string            `json:"command"`
	Image     string            `json:"image"`
	Env       map[string]string `json:"env"`
	Priority  int               `json:"priority"` // 0-10, higher = more priority
	UserID    string            `json:"user_id"`
	Name      string            `json:"name"`       // Job name
	TimeLimit int64             `json:"time_limit"` // In seconds
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid JSON", "details": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Validate command
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		http.Error(w, `{"error": "Validation failed", "details": "command is required"}`, http.StatusBadRequest)
		return
	}

	// Validate command length
	if len(req.Command) > 10000 {
		http.Error(w, `{"error": "Validation failed", "details": "command exceeds maximum length of 10000 characters"}`, http.StatusBadRequest)
		return
	}

	// Default image if not provided
	if req.Image == "" {
		req.Image = "alpine"
	}

	// Validate image name (basic check)
	req.Image = strings.TrimSpace(req.Image)
	if req.Image == "" || strings.Contains(req.Image, " ") {
		http.Error(w, `{"error": "Validation failed", "details": "invalid image name"}`, http.StatusBadRequest)
		return
	}

	// Validate env vars
	for key, value := range req.Env {
		if strings.Contains(key, "=") || strings.Contains(key, " ") {
			http.Error(w, `{"error": "Validation failed", "details": "invalid environment variable name: `+key+`"}`, http.StatusBadRequest)
			return
		}
		if len(value) > 10000 {
			http.Error(w, `{"error": "Validation failed", "details": "environment variable `+key+` exceeds maximum length"}`, http.StatusBadRequest)
			return
		}
	}

	// Validate priority (0-10)
	if req.Priority < 0 || req.Priority > 10 {
		http.Error(w, `{"error": "Validation failed", "details": "priority must be between 0 and 10"}`, http.StatusBadRequest)
		return
	}

	// Create Agent Job definition
	configBytes, _ := json.Marshal(map[string]interface{}{
		"image":       req.Image,
		"commands":    []string{req.Command},
		"environment": req.Env,
		"priority":    req.Priority,
	})

	job, err := s.jobService.CreateJob(r.Context(), req.UserID, req.Name, req.Image, configBytes, req.Priority, req.TimeLimit)
	if err != nil {
		http.Error(w, `{"error": "Failed to create job", "details": "`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Broadcast new job event
	s.hub.Broadcast(Message{
		Type:    "job_created",
		Payload: job,
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	// Parse pagination params
	offset := 0
	limit := 20

	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	// Parse agentID if provided
	var agentID string
	agentIDStr := r.URL.Query().Get("agent_id")
	if agentIDStr != "" {
		agentID = agentIDStr
	}

	var result *services.PaginatedJobs
	var err error
	if agentID != "" {
		result, err = s.jobService.ListJobsByAgent(r.Context(), agentID, offset, limit)
	} else {
		result, err = s.jobService.ListJobs(r.Context(), offset, limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.jobService.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleGetJobLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.jobService.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(job.Logs))
}

type AgentResponse struct {
	*domain.Agent
	ActiveJobCount int64 `json:"active_job_count"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	// Get active agents from memory (60 second timeout)
	agents := s.grpcServer.GetActiveAgents(60 * time.Second)

	var response []*AgentResponse
	for _, agent := range agents {
		count, err := s.jobService.GetActiveJobCount(r.Context(), agent.ID)
		if err != nil {
			// Log error but continue
			count = 0
		}
		response = append(response, &AgentResponse{
			Agent:          agent,
			ActiveJobCount: count,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.jobService.CancelJob(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Broadcast cancel event
	s.hub.Broadcast(Message{
		Type:    "job_cancelled",
		Payload: map[string]string{"job_id": id},
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled", "job_id": id})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := s.jobService.RetryJob(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Broadcast retry event
	s.hub.Broadcast(Message{
		Type:    "job_retried",
		Payload: job,
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleRestartAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.grpcServer.SendCommand(id, "restart") {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "restart command sent"})
	} else {
		http.Error(w, "Agent not active or command channel full", http.StatusServiceUnavailable)
	}
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.grpcServer.SendCommand(id, "update") {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "update command sent"})
	} else {
		http.Error(w, "Agent not active or command channel full", http.StatusServiceUnavailable)
	}
}
