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
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/api/health/detailed", s.handleDetailedHealth)
	s.router.Get("/api/ws", s.handleWS)
	s.router.Get("/api/agents", s.handleListAgents)

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
	Command string            `json:"command"`
	Image   string            `json:"image"`
	Env     map[string]string `json:"env"`
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

	// Create Agent Job definition
	configBytes, _ := json.Marshal(map[string]interface{}{
		"image":       req.Image,
		"commands":    []string{req.Command},
		"environment": req.Env,
	})

	job, err := s.jobService.CreateJob(r.Context(), req.Image, configBytes)
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

	result, err := s.jobService.ListJobs(r.Context(), offset, limit)
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

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	// Get active agents from memory (60 second timeout)
	agents := s.grpcServer.GetActiveAgents(60 * time.Second)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
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
