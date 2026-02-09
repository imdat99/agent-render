package grpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/services"
	"picpic.render/proto"
)

// Auth component
// ... existing imports ...

type Server struct {
	proto.UnimplementedWoodpeckerServer
	proto.UnimplementedWoodpeckerAuthServer
	jobService   *services.JobService
	agentManager *AgentManager
	sessions     sync.Map // map[string]int64 (Token -> AgentID)

	// Track which jobs are assigned to which agents for cleanup on disconnect
	agentJobs sync.Map // map[int64]map[string]bool (AgentID -> Set of JobIDs)
}

// ... NewServer ...
func NewServer(jobService *services.JobService) *Server {
	return &Server{
		jobService:   jobService,
		agentManager: NewAgentManager(),
	}
}

func (s *Server) Version(ctx context.Context, req *proto.Empty) (*proto.VersionResponse, error) {
	return &proto.VersionResponse{
		GrpcVersion:   15, // Check latest compatibility
		ServerVersion: "0.0.1",
	}, nil
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) getAgentIDFromContext(ctx context.Context) (int64, string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return 0, "", false
	}
	tokens := md.Get("token")
	if len(tokens) == 0 {
		return 0, "", false
	}
	token := tokens[0]
	if id, ok := s.sessions.Load(token); ok {
		return id.(int64), token, true
	}
	return 0, "", false
}

func (s *Server) Next(ctx context.Context, req *proto.NextRequest) (*proto.NextResponse, error) {
	agentID, _, ok := s.getAgentIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid or missing token")
	}

	// Update agent heartbeat in memory
	s.agentManager.UpdateHeartbeat(agentID)

	// Capture hostname from Filter labels if present and agent name is still "Pending Agent"
	if req.Filter != nil && req.Filter.Labels != nil {
		if hostname, exists := req.Filter.Labels["hostname"]; exists && hostname != "" {
			// Check if agent still has the placeholder name
			agent, err := s.jobService.GetAgent(ctx, agentID)
			if err == nil && agent != nil && agent.Name == "Pending Agent" {
				// Update agent name with hostname
				log.Printf("Updating agent ID=%d name to hostname: %s", agentID, hostname)
				if _, err := s.jobService.UpdateAgentInfo(ctx, agent.ID, hostname, agent.Platform, agent.Backend, agent.Version, agent.Capacity); err != nil {
					log.Printf("Failed to update agent name: %v", err)
				}
			}
		}
	}

	// Create a context with timeout to avoid blocking forever if no jobs
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	job, err := s.jobService.GetNextJob(ctxWithTimeout)
	if err != nil {
		// If timeout/no job, return default error to tell agent to retry later
		return nil, status.Error(codes.NotFound, "no work")
	}

	log.Printf("Assigning job %s to agent %d", job.ID, agentID)

	// Track job assignment to agent (for cleanup on disconnect)
	s.trackJobAssignment(agentID, job.ID)

	// Parse job config JSON
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(job.Config), &config); err != nil {
		log.Printf("Failed to parse job config for job %s: %v", job.ID, err)
		// Mark job as failed and return error
		_ = s.jobService.UpdateJobStatus(ctx, job.ID, domain.JobStatusFailure)
		s.untrackJobAssignment(agentID, job.ID)
		return nil, status.Error(codes.Internal, "invalid job configuration")
	}

	// Extract image and commands
	image, _ := config["image"].(string)
	if image == "" {
		image = "alpine"
	}

	// Extract commands
	var commands []string
	if cmdList, ok := config["commands"].([]interface{}); ok {
		for _, cmd := range cmdList {
			if cmdStr, ok := cmd.(string); ok {
				commands = append(commands, cmdStr)
			}
		}
	}
	if len(commands) == 0 {
		commands = []string{"echo 'No commands specified'"}
	}

	// Build simple job payload for custom agent
	jobPayload := map[string]interface{}{
		"image":       image,
		"commands":    commands,
		"environment": map[string]string{},
	}

	payloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		log.Printf("Failed to marshal job payload for job %s: %v", job.ID, err)
		s.untrackJobAssignment(agentID, job.ID)
		return nil, status.Error(codes.Internal, "failed to create job payload")
	}

	// Debug: log the payload being sent
	log.Printf("Sending job payload for %s: %s", job.ID, string(payloadBytes))

	// Convert Job to Woodpecker Workflow
	return &proto.NextResponse{
		Workflow: &proto.Workflow{
			Id:      job.ID,
			Timeout: 3600,
			Payload: payloadBytes,
		},
	}, nil
}

func (s *Server) Init(ctx context.Context, req *proto.InitRequest) (*proto.Empty, error) {
	if err := s.jobService.UpdateJobStatus(ctx, req.Id, domain.JobStatusRunning); err != nil {
		log.Printf("Failed to update job %s status to running: %v", req.Id, err)
		return nil, status.Error(codes.Internal, "failed to update job status")
	}
	log.Printf("Workflow %s initialized", req.Id)
	return &proto.Empty{}, nil
}

func (s *Server) Wait(ctx context.Context, req *proto.WaitRequest) (*proto.WaitResponse, error) {
	// No approval needed, workflows run immediately
	return &proto.WaitResponse{Canceled: false}, nil
}

func (s *Server) Done(ctx context.Context, req *proto.DoneRequest) (*proto.Empty, error) {
	agentID, _, ok := s.getAgentIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}

	jobStatus := domain.JobStatusSuccess
	if req.State != nil && req.State.Error != "" {
		jobStatus = domain.JobStatusFailure
		log.Printf("Workflow %s failed: %s", req.Id, req.State.Error)
	}

	if err := s.jobService.UpdateJobStatus(ctx, req.Id, jobStatus); err != nil {
		log.Printf("Failed to update job %s status: %v", req.Id, err)
		return nil, status.Error(codes.Internal, "failed to update job status")
	}

	// Untrack job from agent
	s.untrackJobAssignment(agentID, req.Id)

	log.Printf("Workflow %s completed with status: %s", req.Id, jobStatus)
	return &proto.Empty{}, nil
}

func (s *Server) Update(ctx context.Context, req *proto.UpdateRequest) (*proto.Empty, error) {
	log.Printf("Step update for workflow %s: step=%s exited=%v exit_code=%d",
		req.Id, req.State.StepUuid, req.State.Exited, req.State.ExitCode)
	return &proto.Empty{}, nil
}

func (s *Server) Log(ctx context.Context, req *proto.LogRequest) (*proto.Empty, error) {
	// Validate session
	if _, _, ok := s.getAgentIDFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}

	// Process log entries - extract workflow ID from first log entry
	if len(req.LogEntries) > 0 {
		for _, entry := range req.LogEntries {
			// StepUuid is the JobID in our simplified model
			jobID := entry.StepUuid
			if jobID != "" {
				if err := s.jobService.ProcessLog(ctx, jobID, entry.Data); err != nil {
					log.Printf("Failed to process log for job %s: %v", jobID, err)
					// Continue processing other logs, don't fail the entire batch
				}
			}
		}
	}
	return &proto.Empty{}, nil
}

func (s *Server) Extend(ctx context.Context, req *proto.ExtendRequest) (*proto.Empty, error) {
	log.Printf("Timeout extension requested for workflow %s", req.Id)
	return &proto.Empty{}, nil
}

func (s *Server) RegisterAgent(ctx context.Context, req *proto.RegisterAgentRequest) (*proto.RegisterAgentResponse, error) {
	// Check if req.Info is nil to prevent panic
	if req.Info == nil {
		log.Printf("Error: RegisterAgent called with nil AgentInfo")
		return nil, status.Error(codes.InvalidArgument, "agent info is required")
	}

	log.Printf("Registering Agent: Platform=%s, Backend=%s, Version=%s, Capacity=%d",
		req.Info.Platform, req.Info.Backend, req.Info.Version, req.Info.Capacity)

	id, _, ok := s.getAgentIDFromContext(ctx)
	if !ok {
		log.Printf("Warning: RegisterAgent called without valid session token")
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}

	// Log if capacity is zero (might indicate an issue)
	if req.Info.Capacity == 0 {
		log.Printf("Warning: Agent reporting Capacity=0, this may indicate configuration issue")
	}

	// Extract hostname from custom labels if provided
	hostname := ""
	if req.Info.CustomLabels != nil {
		if h, exists := req.Info.CustomLabels["hostname"]; exists && h != "" {
			hostname = h
			log.Printf("Extracted hostname from customLabels: %s", hostname)
		}
	}

	// Use hostname as name, fallback to "agent-{id}" if not provided
	name := hostname
	if name == "" {
		name = fmt.Sprintf("agent-%d", id)
	}

	// Register agent in memory (no database)
	s.agentManager.Register(id, name, req.Info.Platform, req.Info.Backend, req.Info.Version, req.Info.Capacity)

	log.Printf("Successfully registered agent ID=%d with Capacity=%d, Name=%s", id, req.Info.Capacity, name)

	return &proto.RegisterAgentResponse{
		AgentId: id,
	}, nil
}

// trackJobAssignment tracks that a job has been assigned to an agent
func (s *Server) trackJobAssignment(agentID int64, jobID string) {
	// Create new map if doesn't exist, or load existing
	jobSetInterface, _ := s.agentJobs.LoadOrStore(agentID, &sync.Map{})
	jobSet, ok := jobSetInterface.(*sync.Map)
	if !ok {
		log.Printf("ERROR: agentJobs contains non-*sync.Map value for agent %d", agentID)
		return
	}
	jobSet.Store(jobID, true)
}

// untrackJobAssignment removes job tracking when job completes
func (s *Server) untrackJobAssignment(agentID int64, jobID string) {
	if jobSetInterface, ok := s.agentJobs.Load(agentID); ok {
		jobSet, ok := jobSetInterface.(*sync.Map)
		if !ok {
			log.Printf("ERROR: agentJobs contains non-*sync.Map value for agent %d", agentID)
			return
		}
		jobSet.Delete(jobID)
	}
}

// getAgentJobs returns all jobs assigned to an agent
func (s *Server) getAgentJobs(agentID int64) []string {
	var jobs []string
	if jobSetInterface, ok := s.agentJobs.Load(agentID); ok {
		jobSet, ok := jobSetInterface.(*sync.Map)
		if !ok {
			log.Printf("ERROR: agentJobs contains non-*sync.Map value for agent %d", agentID)
			return jobs
		}
		jobSet.Range(func(key, value interface{}) bool {
			jobs = append(jobs, key.(string))
			return true
		})
	}
	return jobs
}

func (s *Server) UnregisterAgent(ctx context.Context, req *proto.Empty) (*proto.Empty, error) {
	agentID, token, ok := s.getAgentIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}

	log.Printf("Agent %d unregistering", agentID)

	// Mark all assigned jobs as failed (agent disconnected unexpectedly)
	jobs := s.getAgentJobs(agentID)
	for _, jobID := range jobs {
		log.Printf("Marking job %s as failed due to agent %d disconnect", jobID, agentID)
		if err := s.jobService.UpdateJobStatus(ctx, jobID, domain.JobStatusFailure); err != nil {
			log.Printf("Failed to update job %s status: %v", jobID, err)
		}
		s.untrackJobAssignment(agentID, jobID)
	}

	// Remove session
	s.sessions.Delete(token)
	s.agentJobs.Delete(agentID)

	return &proto.Empty{}, nil
}

func (s *Server) ReportHealth(ctx context.Context, req *proto.ReportHealthRequest) (*proto.Empty, error) {
	agentID, _, ok := s.getAgentIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid session")
	}

	// Update heartbeat on health report in memory
	s.agentManager.UpdateHeartbeat(agentID)

	return &proto.Empty{}, nil
}

// Auth component
func (s *Server) Auth(ctx context.Context, req *proto.AuthRequest) (*proto.AuthResponse, error) {
	log.Printf("Agent Auth Request: Token=%s, ID=%d", req.AgentToken, req.AgentId)

	var agent *domain.Agent
	var err error

	// 1. Try to reuse existing agent if ID provided
	if req.AgentId > 0 {
		// Verify if this agent exists in our DB
		existing, err := s.jobService.GetAgent(ctx, req.AgentId)
		if err == nil && existing != nil {
			log.Printf("Reusing existing Agent ID: %d", existing.ID)
			agent = existing
			// Update heartbeat immediately
			_ = s.jobService.UpdateAgentHeartbeat(ctx, agent.ID)
		} else {
			log.Printf("Agent requested ID %d but not found or error: %v", req.AgentId, err)
		}
	}

	// 2. If no valid agent found, create a new one
	if agent == nil {
		agent, err = s.jobService.CreatePlaceholderAgent(ctx)
		if err != nil {
			log.Printf("Failed to create agent: %v", err)
			return nil, status.Error(codes.Internal, "failed to create agent session")
		}
		log.Printf("Created new Agent ID: %d", agent.ID)
	}

	// 3. Generate session
	accessToken := generateToken()
	s.sessions.Store(accessToken, agent.ID)

	return &proto.AuthResponse{
		Status:      "ok",
		AgentId:     agent.ID,
		AccessToken: accessToken,
	}, nil
}
