package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"picpic.render/proto"
)

type Agent struct {
	client     proto.WoodpeckerClient
	authClient proto.WoodpeckerAuthClient
	conn       *grpc.ClientConn
	secret     string
	token      string
	capacity   int
	agentID    int64
	docker     *DockerExecutor

	// Concurrency control
	semaphore  chan struct{}      // Limits concurrent job execution
	wg         sync.WaitGroup     // Tracks running jobs for graceful shutdown
	cancelFunc context.CancelFunc // For canceling running jobs on shutdown

	// Job tracking for cancellation
	activeJobs sync.Map // map[string]context.CancelFunc (JobID -> CancelFunc)
}

type JobPayload struct {
	Image       string            `json:"image"`
	Commands    []string          `json:"commands"`
	Environment map[string]string `json:"environment"`
}

func New(serverAddr, secret string, capacity int) (*Agent, error) {
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	docker, err := NewDockerExecutor()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker executor: %w", err)
	}

	return &Agent{
		client:     proto.NewWoodpeckerClient(conn),
		authClient: proto.NewWoodpeckerAuthClient(conn),
		conn:       conn,
		secret:     secret,
		capacity:   capacity,
		docker:     docker,
		semaphore:  make(chan struct{}, capacity), // Initialize semaphore with capacity
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	// Create a cancellable context for job execution
	jobCtx, cancel := context.WithCancel(ctx)
	a.cancelFunc = cancel
	defer cancel()

	// Retry registration loop
	for {
		if err := a.register(ctx); err != nil {
			log.Printf("Registration failed, retrying in 5s: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		break
	}

	log.Printf("Agent started with ID: %d, Capacity: %d", a.agentID, a.capacity)

	// Start cancel listener in background
	go a.cancelListener(jobCtx)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Context cancelled, initiating graceful shutdown...")
			// Cancel all running jobs
			cancel()
			// Wait for all jobs to complete with timeout
			done := make(chan struct{})
			go func() {
				a.wg.Wait()
				close(done)
			}()
			select {
			case <-done:
				log.Println("All jobs completed gracefully")
			case <-time.After(30 * time.Second):
				log.Println("Shutdown timeout reached, some jobs may still be running")
			}
			// Unregister from server
			if err := a.unregister(ctx); err != nil {
				log.Printf("Failed to unregister agent: %v", err)
			}
			return nil
		case <-ticker.C:
			if err := a.poll(jobCtx); err != nil {
				log.Printf("Error polling for jobs: %v", err)
			}
		}
	}
}

// cancelListener listens for job cancellation signals from the server
func (a *Agent) cancelListener(ctx context.Context) {
	// This would typically subscribe to a Redis channel or similar
	// For now, we'll skip the implementation as it requires additional infrastructure
	// The agent can still cancel jobs locally via the CancelJob method
}

func (a *Agent) register(ctx context.Context) error {
	// 1. Authenticate
	authResp, err := a.authClient.Auth(ctx, &proto.AuthRequest{
		AgentToken: a.secret,
	})
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	a.agentID = authResp.AgentId

	// Create context with token metadata for subsequent calls
	// We need to store this context or recreate it for every call.
	// Since Run passes ctx to poll, we should probably update the client to use an interceptor
	// or just wrap the context in Run loop.
	// For simplicity, let's return the token and let Run handle it,
	// OR store the token in Agent struct and use a helper to get context.
	// But `register` is called once.
	// Let's modify `register` to return token, or store it in Agent.
	// Let's add `token` field to Agent struct.
	a.token = authResp.AccessToken

	// Create context with metadata for Registration
	mdCtx := metadata.AppendToOutgoingContext(ctx, "token", a.token)

	// Get actual hostname instead of hardcoding
	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("Failed to get hostname: %v, using default", err)
		hostname = "unknown-agent"
	}

	// 2. Register
	_, err = a.client.RegisterAgent(mdCtx, &proto.RegisterAgentRequest{
		Info: &proto.AgentInfo{
			Platform: "linux/amd64",
			Backend:  "docker",
			Version:  "custom-v1",
			Capacity: int32(a.capacity),
			CustomLabels: map[string]string{
				"hostname": hostname,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	return nil
}

// Helper to add token to context
func (a *Agent) withToken(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "token", a.token)
}

func (a *Agent) poll(ctx context.Context) error {
	// Try to acquire semaphore without blocking to check if we have capacity
	select {
	case a.semaphore <- struct{}{}:
		// We have capacity, release it back since we'll acquire again in executeJob
		<-a.semaphore
	default:
		// At capacity, skip polling
		log.Printf("At capacity (%d/%d jobs running), skipping poll", len(a.semaphore), a.capacity)
		return nil
	}

	// Get actual hostname for filter
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-agent"
	}

	// Request next job
	mdCtx := a.withToken(ctx)
	resp, err := a.client.Next(mdCtx, &proto.NextRequest{
		Filter: &proto.Filter{
			Labels: map[string]string{
				"hostname": hostname,
			},
		},
	})
	if err != nil {
		return err
	}

	if resp.Workflow == nil {
		return nil
	}

	log.Printf("Received job: %s (active: %d/%d)", resp.Workflow.Id, len(a.semaphore), a.capacity)

	// Start job execution in background with semaphore control
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// Acquire semaphore (blocks if at capacity)
		select {
		case a.semaphore <- struct{}{}:
			defer func() { <-a.semaphore }() // Release when done
		case <-ctx.Done():
			return // Context cancelled, don't start new job
		}

		a.executeJob(ctx, resp.Workflow)
	}()

	return nil
}

func (a *Agent) executeJob(ctx context.Context, workflow *proto.Workflow) {
	log.Printf("Executing job %s", workflow.Id)

	// Create cancellable context for this job
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	// Register job for cancellation tracking
	a.activeJobs.Store(workflow.Id, jobCancel)
	defer a.activeJobs.Delete(workflow.Id)

	// 1. Parse Payload
	var payload JobPayload
	if err := json.Unmarshal(workflow.Payload, &payload); err != nil {
		log.Printf("Failed to parse payload for job %s: %v", workflow.Id, err)
		a.reportDone(ctx, workflow.Id, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	// 2. Init
	mdCtx := a.withToken(ctx)
	if _, err := a.client.Init(mdCtx, &proto.InitRequest{Id: workflow.Id}); err != nil {
		log.Printf("Failed to init job %s: %v", workflow.Id, err)
		return
	}

	// 3. Run Docker
	log.Printf("Running container with image: %s", payload.Image)

	// Create a done channel to detect cancellation
	done := make(chan error, 1)
	go func() {
		done <- a.docker.Run(jobCtx, payload.Image, payload.Commands, payload.Environment, func(line string) {
			// Stream logs
			if _, err := a.client.Log(mdCtx, &proto.LogRequest{
				LogEntries: []*proto.LogEntry{
					{
						StepUuid: workflow.Id,
						Data:     []byte(line),
						Time:     time.Now().Unix(),
						Type:     1, // stdout
					},
				},
			}); err != nil {
				log.Printf("Failed to send log for job %s: %v", workflow.Id, err)
			}
		})
	}()

	// Wait for job completion or cancellation
	var err error
	select {
	case err = <-done:
		// Job completed normally
	case <-jobCtx.Done():
		// Job was cancelled
		err = fmt.Errorf("job cancelled")
		log.Printf("Job %s was cancelled", workflow.Id)
	}

	// 4. Report Done
	if err != nil {
		log.Printf("Job %s failed: %v", workflow.Id, err)
		a.reportDone(ctx, workflow.Id, err.Error())
	} else {
		log.Printf("Job %s succeeded", workflow.Id)
		a.reportDone(ctx, workflow.Id, "")
	}
}

// CancelJob cancels a running job by ID
func (a *Agent) CancelJob(jobID string) bool {
	if cancelFunc, ok := a.activeJobs.Load(jobID); ok {
		log.Printf("Cancelling job %s", jobID)
		cancelFunc.(context.CancelFunc)()
		return true
	}
	return false
}

func (a *Agent) reportDone(ctx context.Context, id string, errStr string) {
	state := &proto.WorkflowState{
		Finished: time.Now().Unix(),
	}
	if errStr != "" {
		state.Error = errStr
		// ExitCode is not in WorkflowState in proto
	}

	// Use token context for authentication
	mdCtx := a.withToken(ctx)
	a.client.Done(mdCtx, &proto.DoneRequest{
		Id:    id,
		State: state,
	})
}

// unregister notifies the server that this agent is going offline
func (a *Agent) unregister(ctx context.Context) error {
	if a.token == "" {
		return nil // Not registered yet
	}

	mdCtx := a.withToken(ctx)
	_, err := a.client.UnregisterAgent(mdCtx, &proto.Empty{})
	if err != nil {
		return fmt.Errorf("failed to unregister agent: %w", err)
	}
	log.Printf("Agent %d unregistered successfully", a.agentID)
	return nil
}
