package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:8080", "agent-1", "cluster-1")
	if client == nil {
		t.Fatal("client should not be nil")
	}
	if client.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL http://localhost:8080, got %s", client.baseURL)
	}
	if client.agentID != "agent-1" {
		t.Errorf("expected agentID agent-1, got %s", client.agentID)
	}
}

func TestSendHeartbeat(t *testing.T) {
	var receivedHeartbeat Heartbeat

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agents/agent-1/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		err := json.NewDecoder(r.Body).Decode(&receivedHeartbeat)
		if err != nil {
			t.Errorf("failed to decode heartbeat: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	heartbeat := &Heartbeat{
		AgentID:            "agent-1",
		ClusterID:          "cluster-1",
		Hostname:           "test-host",
		IsLeader:           true,
		Timestamp:          time.Now(),
		ManagedJobs:        5,
		ManagedDeployments: 3,
	}

	err := client.SendHeartbeat(context.Background(), heartbeat)
	if err != nil {
		t.Fatalf("SendHeartbeat failed: %v", err)
	}

	if receivedHeartbeat.AgentID != "agent-1" {
		t.Errorf("expected agent_id agent-1, got %s", receivedHeartbeat.AgentID)
	}
	if receivedHeartbeat.ManagedJobs != 5 {
		t.Errorf("expected 5 managed jobs, got %d", receivedHeartbeat.ManagedJobs)
	}
}

func TestFetchCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/agent-1/commands" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		commands := []json.RawMessage{
			json.RawMessage(`{"type":"start_pipeline","pipeline_id":"p1"}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commands)
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	commands, err := client.FetchCommands(context.Background())
	if err != nil {
		t.Fatalf("FetchCommands failed: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}
}

func TestFetchCommandsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	commands, err := client.FetchCommands(context.Background())
	if err != nil {
		t.Fatalf("FetchCommands should not error on 204: %v", err)
	}

	if commands != nil {
		t.Errorf("expected nil commands on 204, got %v", commands)
	}
}

func TestReportEvent(t *testing.T) {
	var receivedEvent AgentEvent

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		_ = json.NewDecoder(r.Body).Decode(&receivedEvent)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	event := &AgentEvent{
		AgentID:   "agent-1",
		ClusterID: "cluster-1",
		Type:      "job_completed",
		Message:   "Job completed successfully",
		Timestamp: time.Now(),
	}

	err := client.ReportEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("ReportEvent failed: %v", err)
	}

	if receivedEvent.Type != "job_completed" {
		t.Errorf("expected event type job_completed, got %s", receivedEvent.Type)
	}
}

func TestServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	err := client.SendHeartbeat(context.Background(), &Heartbeat{})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestReportPipelineRunStatus(t *testing.T) {
	var receivedUpdate PipelineRunUpdate

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipeline-runs/run-123/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		_ = json.NewDecoder(r.Body).Decode(&receivedUpdate)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "agent-1", "cluster-1")

	update := &PipelineRunUpdate{
		Status:          "running",
		K8sResourceType: "job",
		K8sResourceName: "conduix-job-wf001-exec001",
		K8sNamespace:    "conduix",
		RunnerImage:     "conduix/runner:latest",
	}

	err := client.ReportPipelineRunStatus(context.Background(), "run-123", update)
	if err != nil {
		t.Fatalf("ReportPipelineRunStatus failed: %v", err)
	}

	if receivedUpdate.K8sResourceType != "job" {
		t.Errorf("expected resource type job, got %s", receivedUpdate.K8sResourceType)
	}
}
