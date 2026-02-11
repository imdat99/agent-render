package grpc

import (
	"testing"
)

func TestAgentManager_Register(t *testing.T) {
	am := NewAgentManager()
	agentID := "agent-12345"
	name := "test-agent"
	platform := "linux/amd64"
	backend := "docker"
	version := "1.0.0"
	capacity := int32(4)

	am.Register(agentID, name, platform, backend, version, capacity)

	info, exists := am.Get(agentID)
	if !exists {
		t.Errorf("Agent with ID %s not found", agentID)
	}
	if info.ID != agentID {
		t.Errorf("Expected AgentID %s, got %s", agentID, info.ID)
	}
	if info.Capacity != capacity {
		t.Errorf("Expected Capacity %d, got %d", capacity, info.Capacity)
	}
}

func TestAgentManager_GetCommandChannel(t *testing.T) {
	am := NewAgentManager()
	agentID := "agent-channel-test"

	am.Register(agentID, "test-agent", "linux", "docker", "1.0", 2)

	ch, exists := am.GetCommandChannel(agentID)
	if !exists {
		t.Errorf("Command channel not found for agent %s", agentID)
	}
	if ch == nil {
		t.Errorf("Command channel is nil")
	}
}
