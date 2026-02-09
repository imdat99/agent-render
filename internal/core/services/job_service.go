package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/ports"
)

type JobService struct {
	jobRepo   ports.JobRepository
	agentRepo ports.AgentRepository
	queue     ports.JobQueue
	pubsub    ports.LogPubSub
}

func NewJobService(
	jobRepo ports.JobRepository,
	agentRepo ports.AgentRepository,
	queue ports.JobQueue,
	pubsub ports.LogPubSub,
) *JobService {
	return &JobService{
		jobRepo:   jobRepo,
		agentRepo: agentRepo,
		queue:     queue,
		pubsub:    pubsub,
	}
}

func (s *JobService) CreateJob(ctx context.Context, image string, config []byte, priority int) (*domain.Job, error) {
	job := &domain.Job{
		ID:        fmt.Sprintf("job-%s", uuid.New().String()), // UUID for unique ID generation
		Status:    domain.JobStatusPending,
		Priority:  priority,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Config:    string(config),
	}

	if err := s.jobRepo.Create(ctx, job); err != nil {
		return nil, err
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

// PaginatedJobs represents a paginated list of jobs with metadata
type PaginatedJobs struct {
	Jobs    []*domain.Job `json:"jobs"`
	Total   int64         `json:"total"`
	Offset  int           `json:"offset"`
	Limit   int           `json:"limit"`
	HasMore bool          `json:"has_more"`
}

func (s *JobService) ListJobs(ctx context.Context, offset, limit int) (*PaginatedJobs, error) {
	// Validate and normalize pagination params
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	jobs, err := s.jobRepo.ListJobs(ctx, offset, limit)
	if err != nil {
		return nil, err
	}

	total, err := s.jobRepo.CountJobs(ctx)
	if err != nil {
		return nil, err
	}

	return &PaginatedJobs{
		Jobs:    jobs,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: offset+len(jobs) < int(total),
	}, nil
}

func (s *JobService) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	return s.jobRepo.GetJob(ctx, id)
}

func (s *JobService) GetAgent(ctx context.Context, id int64) (*domain.Agent, error) {
	return s.agentRepo.GetAgent(ctx, id)
}

func (s *JobService) GetAgentByName(ctx context.Context, name string) (*domain.Agent, error) {
	return s.agentRepo.GetAgentByName(ctx, name)
}

func (s *JobService) CreatePlaceholderAgent(ctx context.Context) (*domain.Agent, error) {
	agent := &domain.Agent{
		Name:          "Pending Agent",
		Status:        domain.AgentStatusOffline,
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
	}
	if err := s.agentRepo.CreateOrUpdate(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func (s *JobService) UpdateAgentInfo(ctx context.Context, id int64, name, platform, backend, version string, capacity int32) (*domain.Agent, error) {
	agent, err := s.agentRepo.GetAgent(ctx, id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("agent not found")
	}

	if name != "" {
		agent.Name = name
	}
	agent.Platform = platform
	agent.Backend = backend
	agent.Version = version
	agent.Capacity = capacity
	agent.LastHeartbeat = time.Now()
	// Status could be set to online here?
	agent.Status = domain.AgentStatusOnline // Transient but might differ in Repo

	return agent, s.agentRepo.CreateOrUpdate(ctx, agent)
}

func (s *JobService) UpdateAgentHeartbeat(ctx context.Context, id int64) error {
	agent, err := s.agentRepo.GetAgent(ctx, id)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("agent not found")
	}
	agent.LastHeartbeat = time.Now()
	return s.agentRepo.CreateOrUpdate(ctx, agent)
}

func (s *JobService) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	return s.agentRepo.ListAgents(ctx)
}

type AgentWithStats struct {
	*domain.Agent
	ActiveJobCount int64 `json:"active_job_count"`
}

func (s *JobService) ListAgentsWithStats(ctx context.Context) ([]*AgentWithStats, error) {
	agents, err := s.agentRepo.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	var result []*AgentWithStats
	for _, agent := range agents {
		count, err := s.jobRepo.CountActiveJobsByAgent(ctx, agent.ID)
		if err != nil {
			log.Printf("Failed to count active jobs for agent %d: %v", agent.ID, err)
			count = 0
		}
		result = append(result, &AgentWithStats{
			Agent:          agent,
			ActiveJobCount: count,
		})
	}
	return result, nil
}

func (s *JobService) ListJobsByAgent(ctx context.Context, agentID int64, offset, limit int) (*PaginatedJobs, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	jobs, err := s.jobRepo.ListJobsByAgent(ctx, agentID, offset, limit)
	if err != nil {
		return nil, err
	}

	total, err := s.jobRepo.CountJobsByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	return &PaginatedJobs{
		Jobs:    jobs,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: offset+len(jobs) < int(total),
	}, nil
}

func (s *JobService) GetActiveJobCount(ctx context.Context, agentID int64) (int64, error) {
	return s.jobRepo.CountActiveJobsByAgent(ctx, agentID)
}

func (s *JobService) GetNextJob(ctx context.Context) (*domain.Job, error) {
	// Blocking dequeue
	return s.queue.Dequeue(ctx)
}

const (
	maxLogLines = 10000            // Maximum number of log lines to store per job
	maxLogSize  = 10 * 1024 * 1024 // 10MB max log size per job
)

func (s *JobService) ProcessLog(ctx context.Context, jobID string, logData []byte) error {
	line := string(logData)

	// Parse progress from ffmpeg output
	// ffmpeg -progress pipe:1 output format: out_time_us=123456
	re := regexp.MustCompile(`out_time_us=(\d+)`)
	matches := re.FindStringSubmatch(line)

	var progress float64
	if len(matches) > 1 {
		us, _ := strconv.ParseInt(matches[1], 10, 64)
		if us > 0 {
			progress = float64(us) / 1000000.0 // Convert to seconds
		}
	}

	// Persist logs to DB with size limits
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job %s: %w", jobID, err)
	}
	if job == nil {
		return fmt.Errorf("job %s not found", jobID)
	}

	// Check log size limits
	currentLogSize := len(job.Logs)
	if currentLogSize >= maxLogSize {
		// Keep last 80% of logs, truncate from beginning
		keepSize := int(float64(maxLogSize) * 0.8)
		if currentLogSize > keepSize {
			// Find a newline to truncate at to avoid cutting mid-line
			truncatePos := currentLogSize - keepSize
			newlinePos := strings.Index(job.Logs[truncatePos:], "\n")
			if newlinePos != -1 {
				truncatePos += newlinePos + 1
			}
			job.Logs = fmt.Sprintf("[...truncated %d bytes...]\n", truncatePos) + job.Logs[truncatePos:]
		}
	}

	// Append new log line
	newLog := line
	if !strings.HasSuffix(line, "\n") {
		newLog += "\n"
	}

	job.Logs += newLog
	if progress > 0 {
		job.Progress = progress
	}
	job.UpdatedAt = time.Now()

	// Update DB first
	if err := s.jobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job logs: %w", err)
	}

	// Only publish to PubSub after successful DB update
	if err := s.pubsub.Publish(ctx, jobID, line, progress); err != nil {
		log.Printf("Failed to publish log to pubsub for job %s: %v", jobID, err)
		// Don't fail the whole operation if pubsub fails
	}

	return nil
}

func (s *JobService) UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error {
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	job.Status = status
	job.UpdatedAt = time.Now()
	if err := s.jobRepo.Update(ctx, job); err != nil {
		return err
	}

	// Publish job update event
	if err := s.pubsub.PublishJobUpdate(ctx, jobID, string(status)); err != nil {
		log.Printf("Failed to publish job update: %v", err)
	}

	return nil
}

func (s *JobService) AssignJob(ctx context.Context, jobID string, agentID int64) error {
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	job.AgentID = agentID
	job.Status = domain.JobStatusRunning // Or submitted? Usually running when assigned or about to run.
	// Actually "submitted" means sent to agent? "running" means agent started it.
	// But let's set it to running for simplicity or "submitted".
	// The problem description says "NODE: Unassigned".
	// If we set AgentID, UI should show it.
	job.UpdatedAt = time.Now()

	if err := s.jobRepo.Update(ctx, job); err != nil {
		return err
	}

	// Publish update so UI sees the assignment immediately
	if err := s.pubsub.PublishJobUpdate(ctx, jobID, string(job.Status)); err != nil {
		log.Printf("Failed to publish job update: %v", err)
	}

	return nil
}

func (s *JobService) CancelJob(ctx context.Context, jobID string) error {
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("job not found: %w", err)
	}

	// Only allow cancelling pending or running jobs
	if job.Status != domain.JobStatusPending && job.Status != domain.JobStatusRunning {
		return fmt.Errorf("cannot cancel job with status %s", job.Status)
	}

	// Mark job as cancelled
	job.Cancelled = true
	job.Status = domain.JobStatusCancelled
	job.UpdatedAt = time.Now()

	if err := s.jobRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	// Publish job update event (status change to Cancelled)
	if err := s.pubsub.PublishJobUpdate(ctx, jobID, string(domain.JobStatusCancelled)); err != nil {
		log.Printf("Failed to publish job update: %v", err)
	}

	// Publish cancellation event to notify agents
	if job.AgentID != 0 {
		if err := s.pubsub.PublishCancel(ctx, job.AgentID, job.ID); err != nil {
			log.Printf("Failed to publish cancel signal: %v", err)
		}
	}

	if err := s.pubsub.Publish(ctx, jobID, "[SYSTEM] Job cancelled by user", -1); err != nil {
		log.Printf("Failed to publish cancel event: %v", err)
	}

	return nil
}

// RetryJob retries a failed or cancelled job
func (s *JobService) RetryJob(ctx context.Context, jobID string) (*domain.Job, error) {
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}

	// Only allow retrying failed or cancelled jobs
	if job.Status != domain.JobStatusFailure && job.Status != domain.JobStatusCancelled {
		return nil, fmt.Errorf("cannot retry job with status %s", job.Status)
	}

	// Check max retries
	if job.RetryCount >= job.MaxRetries {
		return nil, fmt.Errorf("max retries (%d) exceeded", job.MaxRetries)
	}

	// Reset job state for retry
	job.Status = domain.JobStatusPending
	job.Cancelled = false
	job.RetryCount++
	job.Progress = 0
	job.UpdatedAt = time.Now()

	if err := s.jobRepo.Update(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update job: %w", err)
	}

	// Re-queue the job
	if err := s.queue.Enqueue(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to re-queue job: %w", err)
	}

	return job, nil
}

func (s *JobService) UpdateJobProgress(ctx context.Context, jobID string, progress float64) error {
	job, err := s.jobRepo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	job.Progress = progress
	job.UpdatedAt = time.Now()

	if err := s.jobRepo.Update(ctx, job); err != nil {
		return err
	}

	// Also publish log entry with progress so dashboard sees it on log stream too?
	// Or maybe the dashboard should listen to a progress channel?
	// The current PubSub.Publish takes progress as arg, so it's sent with log line.
	// But here we have no log line.
	// We can send empty log line.
	return s.pubsub.Publish(ctx, jobID, "", progress)
}

func (s *JobService) PublishSystemResources(ctx context.Context, agentID int64, data []byte) error {
	return s.pubsub.PublishResource(ctx, agentID, data)
}

func (s *JobService) SubscribeSystemResources(ctx context.Context) (<-chan domain.SystemResource, error) {
	return s.pubsub.SubscribeResources(ctx)
}

func (s *JobService) SubscribeCancel(ctx context.Context, agentID int64) (<-chan string, error) {
	return s.pubsub.SubscribeCancel(ctx, agentID)
}

func (s *JobService) SubscribeJobUpdates(ctx context.Context) (<-chan string, error) {
	return s.pubsub.SubscribeJobUpdates(ctx)
}
