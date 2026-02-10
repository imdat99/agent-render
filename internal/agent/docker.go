package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type DockerExecutor struct {
	cli *client.Client
}

func NewDockerExecutor() (*DockerExecutor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerExecutor{cli: cli}, nil
}

// demultiplexStream reads Docker's multiplexed log format and returns stdout/stderr lines
// Docker multiplexed format: [8-byte header][data]
// Header: [1 byte stream type][3 bytes padding][4 bytes size]
// Stream type: 1 = stdout, 2 = stderr
func demultiplexStream(r io.Reader, logCallback func(string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		// Need at least 8 bytes for header
		if len(data) < 8 {
			return 0, nil, nil
		}

		// Read header
		header := data[:8]
		_ = header[0] // streamType: 1 = stdout, 2 = stderr (not used but kept for reference)
		size := binary.BigEndian.Uint32(header[4:8])

		// Check if we have the full message
		totalSize := 8 + int(size)
		if len(data) < totalSize {
			return 0, nil, nil
		}

		// Extract payload
		payload := data[8:totalSize]
		return totalSize, payload, nil
	})

	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			logCallback(line)
		}
	}

	return scanner.Err()
}

func (d *DockerExecutor) Run(ctx context.Context, imageName string, commands []string, env map[string]string, logCallback func(string)) error {
	// 1. Pull Image
	reader, err := d.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		log.Printf("Warning: Failed to pull image %s (might exist locally): %v", imageName, err)
		// Proceed without pulling
	} else {
		// Consume reader to ensure pull completes (and avoid leaking connection)
		io.Copy(io.Discard, reader)
		reader.Close()
	}

	// 2. Configure Container
	// Convert env map to slice
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}

	// Command construction
	script := strings.Join(commands, " && ")
	if len(commands) == 0 {
		script = "echo 'No commands to execute'"
	}

	cmd := []string{"/bin/sh", "-c", script}

	resp, err := d.cli.ContainerCreate(ctx, &container.Config{
		Image:      imageName,
		Entrypoint: cmd, // Override image's ENTRYPOINT (e.g., ffmpeg) to run shell
		Cmd:        nil, // Clear CMD as we use Entrypoint
		Env:        envSlice,
		Tty:        true, // Merge stdout/stderr for simpler log streaming
	}, nil, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	containerID := resp.ID
	defer d.cleanup(context.Background(), containerID)

	// 3. Start Container
	if err := d.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// 4. Stream Logs
	// Use a WaitGroup to ensure we read all logs before returning (and cleaning up container)
	var wg sync.WaitGroup

	out, err := d.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		log.Printf("Failed to get logs: %v", err)
	} else {
		wg.Add(1)
		// Read logs in background
		go func() {
			defer wg.Done()
			defer out.Close()
			scanner := bufio.NewScanner(out)
			for scanner.Scan() {
				logCallback(scanner.Text())
			}
		}()
	}

	// Wait for container
	statusCh, errCh := d.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			// Wait for logs to finish naturally as container has exited
			wg.Wait()
			return fmt.Errorf("container exited with code %d", status.StatusCode)
		}
	}

	// Wait for logs to finish
	wg.Wait()

	return nil
}

func (d *DockerExecutor) cleanup(ctx context.Context, containerID string) {
	d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// SelfUpdate spawns a Watchtower container to update the current container
func (d *DockerExecutor) SelfUpdate(ctx context.Context, imageTag string, agentID int64) error {
	log.Printf("Initiating self-update using Watchtower...")

	// 1. Get current container ID (hostname)
	containerID, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname (container ID): %w", err)
	}
	log.Printf("Current Container ID: %s", containerID)

	// 2. Pull Watchtower image
	watchtowerImage := "containrrr/watchtower:latest"
	reader, err := d.cli.ImagePull(ctx, watchtowerImage, image.PullOptions{})
	if err != nil {
		log.Printf("Failed to pull watchtower: %v", err)
		return fmt.Errorf("failed to pull watchtower: %w", err)
	}
	io.Copy(io.Discard, reader)
	reader.Close()

	// 3. Configure Watchtower
	// We map the Docker socket and tell it to update this specific container
	// and clean up old images.
	// watchtower --run-once --cleanup <container_id>

	hostSock := os.Getenv("HOST_DOCKER_SOCK")
	if hostSock == "" {
		hostSock = "/var/run/docker.sock"
	}

	cmd := []string{"/watchtower", "--run-once", "--cleanup", "--debug", containerID}

	resp, err := d.cli.ContainerCreate(ctx, &container.Config{
		Image: watchtowerImage,
		Cmd:   cmd,
	}, &container.HostConfig{
		Binds: []string{fmt.Sprintf("%s:/var/run/docker.sock", hostSock)},
	}, nil, nil, fmt.Sprintf("watchtower-updater-%d-%d", agentID, time.Now().Unix()))

	if err != nil {
		return fmt.Errorf("failed to create watchtower container: %w", err)
	}

	// 4. Start Watchtower
	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start watchtower: %w", err)
	}

	log.Printf("Watchtower started with ID: %s. Monitoring...", resp.ID)

	// We don't wait for it, because it will kill us (the agent container).
	// But we can wait a bit to see if it starts successfully.
	time.Sleep(2 * time.Second)

	return nil
}
