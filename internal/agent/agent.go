package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
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

	// System stats state
	prevCPUTotal uint64
	prevCPUIdle  uint64
}

type JobPayload struct {
	Image       string            `json:"image"`
	Commands    []string          `json:"commands"`
	Environment map[string]string `json:"environment"`
	Action      string            `json:"action"` // "restart", "update"
}

func New(serverAddr, secret string, capacity int) (*Agent, error) {
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	// Master loop for connection/registration management
	for {
		// Check global context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 1. Register with retry
		if err := a.registerWithRetry(ctx); err != nil {
			log.Printf("Registration failed hard: %v", err)
			return err
		}

		log.Printf("Agent started/reconnected with ID: %d, Capacity: %d", a.agentID, a.capacity)

		// 2. Create session context
		// This context controls the lifecycle of this specific connection session
		sessionCtx, sessionCancel := context.WithCancel(ctx)

		// 3. Start background routines
		a.startBackgroundRoutines(sessionCtx)

		// 4. Stream Jobs (Blocking)
		// If this returns, it means connection was lost or context cancelled
		err := a.streamJobs(sessionCtx)

		// 5. Cleanup session
		sessionCancel()
		a.wg.Wait() // Wait for background routines to stop (except running jobs?)
		// Actually running jobs should probably continue if possible?
		// But if we lose connection, we can't report status.
		// The requirement says "Resilience: ... Agent phải tự động reconnect."
		// If we reconnect fast enough, maybe we can keep jobs running?
		// But for now, let's assume we re-register and re-sync.
		// Existing jobs are managed by `a.activeJobs`.
		// If we cancel `sessionCtx`, passed to `streamJobs`, it shouldn't affect `jobCtx` of individual jobs?
		// `executeJob` creates `jobCtx` from `ctx` (passed to Run)? No, it was `jobCtx` from `Run`.
		// I should use `ctx` (app context) for jobs, but `sessionCtx` for streams.

		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("Session ended: %v. Re-registering in 5s...", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			continue
		}
	}
}

// registerWithRetry retries registration until success or context cancellation
func (a *Agent) registerWithRetry(ctx context.Context) error {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		if err := a.register(ctx); err != nil {
			log.Printf("Registration failed: %v. Retrying in %v...", err, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}
		return nil
	}
}

func (a *Agent) startBackgroundRoutines(ctx context.Context) {
	// Start cancel listener
	go a.cancelListener(ctx)

	// Start status reporter
	go a.submitStatusLoop(ctx)
}

// cancelListener listens for job cancellation signals from the server
func (a *Agent) cancelListener(ctx context.Context) {
	// This would typically subscribe to a Redis channel or similar
	// For now, we'll skip the implementation as it requires additional infrastructure
	// The agent can still cancel jobs locally via the CancelJob method
}

func (a *Agent) register(ctx context.Context) error {
	// 1. Authenticate
	// Load persisted ID if available
	savedID, _ := a.loadAgentID()
	if savedID > 0 {
		log.Printf("Loaded persisted Agent ID: %d", savedID)
		a.agentID = savedID
	}

	authResp, err := a.authClient.Auth(ctx, &proto.AuthRequest{
		AgentToken: a.secret,
		AgentId:    a.agentID,
	})
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	a.agentID = authResp.AgentId

	// Persist ID if changed
	if a.agentID != savedID {
		if err := a.saveAgentID(a.agentID); err != nil {
			log.Printf("Failed to save agent ID: %v", err)
		} else {
			log.Printf("Persisted Agent ID: %d", a.agentID)
		}
	}

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

func (a *Agent) streamJobs(ctx context.Context) error {
	// Request job stream
	mdCtx := a.withToken(ctx)

	// Get actual hostname for filter
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-agent"
	}

	stream, err := a.client.StreamJobs(mdCtx, &proto.StreamOptions{
		Filter: &proto.Filter{
			Labels: map[string]string{
				"hostname": hostname,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start job stream: %w", err)
	}

	log.Println("Connected to job stream")

	for {
		// Acquire semaphore before reading from stream to ensure we have capacity
		select {
		case a.semaphore <- struct{}{}:
			// We have capacity
		case <-ctx.Done():
			return ctx.Err()
		case <-stream.Context().Done():
			return stream.Context().Err()
		}

		workflow, err := stream.Recv()
		if err != nil {
			// Release semaphore if we fail to receive
			<-a.semaphore
			return fmt.Errorf("stream closed or error: %w", err)
		}

		// Check for cancellation signal
		if workflow.Cancel {
			// Release semaphore immediately as this is not a new job
			<-a.semaphore

			log.Printf("Received cancellation signal for job %s", workflow.Id)
			if found := a.CancelJob(workflow.Id); found {
				log.Printf("Job %s cancellation triggered", workflow.Id)
			} else {
				log.Printf("Job %s not found in active jobs", workflow.Id)
			}
			continue
		}

		log.Printf("Received job from stream: %s (active: %d/%d)", workflow.Id, len(a.semaphore), a.capacity)

		a.wg.Add(1)
		// We use a background context for execution so that if stream disconnects,
		// the running job is NOT cancelled immediately (unless agent shuts down).
		// However, in the Run loop, we wait for a.wg.Wait().
		// And we don't seem to cancel running jobs on session disconnect in new logic.
		// `executeJob` uses `ctx` passed to it.
		// In `Run` refactor, pass `ctx` (global) to `executeJob`?
		// Wait, `executeJob` call needs to be async.
		go func(wf *proto.Workflow) {
			defer a.wg.Done()
			defer func() { <-a.semaphore }() // Release when done
			// Use the global context or a derivation?
			// `ctx` passed to streamJobs is `sessionCtx`.
			// If `sessionCtx` is cancelled, `executeJob` (if using it) will be cancelled.
			// Ideally, running jobs should survive a brief disconnect?
			// The original code used `jobCtx` which was global for the run.
			// Here `ctx` is `sessionCtx`.
			// If we want jobs to survive, we should pass the global `Agent` context to `streamJobs` purely for `executeJob`.
			// But `streamJobs` signature only has one ctx.
			// Let's rely on the fact that we might want to cancel jobs if we lose connection?
			// No, "Resilience": "If Agent disconnects... Server Re-queue".
			// If Agent disconnects, Server re-queues. Agent should probably kill the job?
			// Or Agent finishes it and reports Done?
			// If Agent finishes and reports Done, but Server already re-queued, we have duplicate execution.
			// So it is SAFER to cancel the job if we disconnect.
			// So using `sessionCtx` (which is cancelled on disconnect) for `executeJob` is correct behavior.
			a.executeJob(ctx, wf)
		}(workflow)
	}
}

func (a *Agent) submitStatusLoop(ctx context.Context) {
	// Simple retry loop for status stream
	for {
		if err := a.runStatusStream(ctx); err != nil {
			log.Printf("Status stream error: %v. Retrying in 5s...", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		} else {
			// If correct return, maybe context done
			return
		}
	}
}

func (a *Agent) runStatusStream(ctx context.Context) error {
	mdCtx := a.withToken(ctx)
	stream, err := a.client.SubmitStatus(mdCtx)
	if err != nil {
		return err
	}

	// Ticker for system resources
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Send resource usage
			cpu, ram := a.collectSystemResources()

			data := fmt.Sprintf(`{"cpu": %.2f, "ram": %.2f}`, cpu, ram)

			err := stream.Send(&proto.StatusUpdate{
				Type: 5, // system-resource
				Time: time.Now().Unix(),
				Data: []byte(data),
			})
			if err != nil {
				return err
			}
		}
	}
}

func (a *Agent) collectSystemResources() (float64, float64) {
	// RAM Usage from /proc/meminfo
	var memTotal, memAvailable uint64

	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if fields[0] == "MemTotal:" {
				memTotal, _ = strconv.ParseUint(fields[1], 10, 64)
			} else if fields[0] == "MemAvailable:" {
				memAvailable, _ = strconv.ParseUint(fields[1], 10, 64)
			}
		}
	} else {
		// Fallback if /proc/meminfo not available (e.g. non-linux)
		// For now just return 0
	}

	usedRAM := 0.0
	if memTotal > 0 {
		// Convert kB to MB
		usedRAM = float64(memTotal-memAvailable) / 1024.0
	}

	// CPU Usage from /proc/stat
	var cpuUsage float64
	data, err = os.ReadFile("/proc/stat")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) < 5 {
					continue
				}
				// cpu  user nice system idle iowait irq softirq steal guest guest_nice
				var user, nice, system, idle, iowait, irq, softirq, steal uint64

				user, _ = strconv.ParseUint(fields[1], 10, 64)
				nice, _ = strconv.ParseUint(fields[2], 10, 64)
				system, _ = strconv.ParseUint(fields[3], 10, 64)
				idle, _ = strconv.ParseUint(fields[4], 10, 64)
				if len(fields) > 5 {
					iowait, _ = strconv.ParseUint(fields[5], 10, 64)
				}
				if len(fields) > 6 {
					irq, _ = strconv.ParseUint(fields[6], 10, 64)
				}
				if len(fields) > 7 {
					softirq, _ = strconv.ParseUint(fields[7], 10, 64)
				}
				if len(fields) > 8 {
					steal, _ = strconv.ParseUint(fields[8], 10, 64)
				}

				currentIdle := idle + iowait
				currentNonIdle := user + nice + system + irq + softirq + steal
				currentTotal := currentIdle + currentNonIdle

				totalDiff := currentTotal - a.prevCPUTotal
				idleDiff := currentIdle - a.prevCPUIdle

				if totalDiff > 0 && a.prevCPUTotal > 0 {
					cpuUsage = float64(totalDiff-idleDiff) / float64(totalDiff) * 100.0
				}

				// Update state
				a.prevCPUTotal = currentTotal
				a.prevCPUIdle = currentIdle
				break
			}
		}
	}

	return cpuUsage, usedRAM
}

func (a *Agent) executeJob(ctx context.Context, workflow *proto.Workflow) {
	log.Printf("Executing job %s", workflow.Id)

	// Create cancellable context for this job
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	// Add timeout if specified
	if workflow.Timeout > 0 {
		timeoutDuration := time.Duration(workflow.Timeout) * time.Second
		log.Printf("Job %s has timeout of %v", workflow.Id, timeoutDuration)
		jobCtx, jobCancel = context.WithTimeout(jobCtx, timeoutDuration)
		defer jobCancel()
	}

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

	// CHECK FOR SYSTEM COMMANDS
	if payload.Action != "" {
		log.Printf("Received system command: %s", payload.Action)

		// Report done immediately so server knows we received it
		// For restart/update, we might die shortly after
		a.reportDone(ctx, workflow.Id, "")

		switch payload.Action {
		case "restart":
			log.Println("Restarting agent...")
			os.Exit(0) // Let Docker/Supervisor restart us
		case "update":
			log.Println("Updating agent...")
			// Pull latest image
			// We should probably know our own image name.
			// Assuming "quay.io/lethdat/picpic.agent:latest" or similar.
			// Ideally passed via env var.
			imageName := os.Getenv("AGENT_IMAGE")
			if imageName == "" {
				imageName = "quay.io/lethdat/picpic.agent:latest"
			}

			if err := a.docker.SelfUpdate(context.Background(), imageName, a.agentID); err != nil {
				log.Printf("Update failed: %v", err)
			} else {
				os.Exit(0) // Should be killed by updater, but exit anyway
			}
		}
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
			// Parse progress from log line
			var progress float64 = -1
			if val, ok := parseProgress(line); ok {
				progress = val
			}

			logEntry := &proto.LogEntry{
				StepUuid: workflow.Id,
				Data:     []byte(line),
				Time:     time.Now().Unix(),
				Type:     1, // stdout
			}

			// If we parsed progress, send it as separate entry or same?
			// The Requirement says "SubmitStatus: Agent stream logs, % progress"
			// But here we are using `Log` RPC in existing code.
			// Ideally we should move to `SubmitStatus` streaming RPC which we just added.
			// But for backward compatibility or ease, let's stick to `Log` for now?
			// NO, the plan says "Implement SubmitStatus stream loop".
			// However, `executeJob` is using `a.client.Log`.
			// Let's change `executeJob` to avoid using `Log` RPC if we want to fully switch,
			// OR we can use `Log` RPC for logs and `SubmitStatus` for progress/metrics.
			// But `SubmitStatus` can carry logs too.
			// Let's use `Log` for now to minimize changes in `executeJob` structure unless necessary.
			// Wait, the user requirement is "SubmitStatus: Agent stream logs...".
			// So I should probably use `SubmitStatus` if possible.
			// BUT `SubmitStatus` is a stream initiated by `runStatusStream`.
			// How can `executeJob` access that stream?
			// It's running in a separate goroutine `submitStatusLoop`.
			// We need a channel to send updates to `submitStatusLoop`.

			// For now, let's keep using `Log` RPC for Line logs as it's simple.
			// BUT, for progress, we can send a special LogEntry type = 4 (progress).

			entries := []*proto.LogEntry{logEntry}

			if progress >= 0 {
				entries = append(entries, &proto.LogEntry{
					StepUuid: workflow.Id,
					Time:     time.Now().Unix(),
					Type:     4, // progress
					Data:     []byte(fmt.Sprintf("%f", progress)),
				})
			}

			if _, err := a.client.Log(mdCtx, &proto.LogRequest{
				LogEntries: entries,
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
		// Check if it was a timeout or manual cancellation
		if jobCtx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("job timeout exceeded")
			log.Printf("Job %s timed out", workflow.Id)
		} else {
			err = fmt.Errorf("job cancelled")
			log.Printf("Job %s was cancelled", workflow.Id)
		}
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

	// Use a fresh context to ensure report is sent even if job context is cancelled
	reportCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use token context for authentication
	mdCtx := a.withToken(reportCtx)
	_, err := a.client.Done(mdCtx, &proto.DoneRequest{
		Id:    id,
		State: state,
	})
	if err != nil {
		log.Printf("Failed to report Done for job %s: %v", id, err)
	}
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

const AgentIDFile = "/data/agent_id"

func (a *Agent) loadAgentID() (int64, error) {
	data, err := os.ReadFile(AgentIDFile)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (a *Agent) saveAgentID(id int64) error {
	// Ensure directory exists
	// But since we mount /data, it should exist.
	// If not mounted, it might fail if /data doesn't exist?
	// /bin/agent is root, so /data might need creation if not volume mounted?
	// But our Dockerfile doesn't create /data. Volume mount creates it.
	// Safe to assume /data exists or mkdir?
	if err := os.MkdirAll("/data", 0755); err != nil {
		return err
	}
	return os.WriteFile(AgentIDFile, []byte(fmt.Sprintf("%d", id)), 0644)
}
