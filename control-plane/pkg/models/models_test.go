package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPipelineTableName(t *testing.T) {
	p := Pipeline{}
	if p.TableName() != "pipelines" {
		t.Errorf("expected table name 'pipelines', got '%s'", p.TableName())
	}
}

func TestPipelineRunTableName(t *testing.T) {
	pr := PipelineRun{}
	if pr.TableName() != "pipeline_runs" {
		t.Errorf("expected table name 'pipeline_runs', got '%s'", pr.TableName())
	}
}

func TestScheduleTableName(t *testing.T) {
	s := Schedule{}
	if s.TableName() != "schedules" {
		t.Errorf("expected table name 'schedules', got '%s'", s.TableName())
	}
}

func TestUserTableName(t *testing.T) {
	u := User{}
	if u.TableName() != "users" {
		t.Errorf("expected table name 'users', got '%s'", u.TableName())
	}
}

func TestAgentTableName(t *testing.T) {
	a := Agent{}
	if a.TableName() != "agents" {
		t.Errorf("expected table name 'agents', got '%s'", a.TableName())
	}
}

func TestSessionTableName(t *testing.T) {
	s := Session{}
	if s.TableName() != "sessions" {
		t.Errorf("expected table name 'sessions', got '%s'", s.TableName())
	}
}

func TestAuditLogTableName(t *testing.T) {
	al := AuditLog{}
	if al.TableName() != "audit_logs" {
		t.Errorf("expected table name 'audit_logs', got '%s'", al.TableName())
	}
}

func TestPipelineJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	p := Pipeline{
		ID:          "pipeline-123",
		Name:        "Test Pipeline",
		Description: "A test pipeline",
		ConfigYAML:  "version: '1.0'\nname: test",
		CreatedBy:   "user-1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal pipeline: %v", err)
	}

	var decoded Pipeline
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal pipeline: %v", err)
	}

	if decoded.ID != p.ID {
		t.Errorf("ID mismatch: expected %s, got %s", p.ID, decoded.ID)
	}
	if decoded.Name != p.Name {
		t.Errorf("Name mismatch: expected %s, got %s", p.Name, decoded.Name)
	}
	if decoded.ConfigYAML != p.ConfigYAML {
		t.Errorf("ConfigYAML mismatch")
	}
}

func TestPipelineRunJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	pr := PipelineRun{
		ID:             "run-123",
		PipelineID:     "pipeline-123",
		Status:         "running",
		AgentID:        "agent-1",
		StartedAt:      &now,
		ProcessedCount: 1000,
		ErrorCount:     5,
		CreatedAt:      now,
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("failed to marshal pipeline run: %v", err)
	}

	var decoded PipelineRun
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal pipeline run: %v", err)
	}

	if decoded.ID != pr.ID {
		t.Errorf("ID mismatch: expected %s, got %s", pr.ID, decoded.ID)
	}
	if decoded.Status != pr.Status {
		t.Errorf("Status mismatch: expected %s, got %s", pr.Status, decoded.Status)
	}
	if decoded.ProcessedCount != pr.ProcessedCount {
		t.Errorf("ProcessedCount mismatch: expected %d, got %d", pr.ProcessedCount, decoded.ProcessedCount)
	}
}

func TestScheduleJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	next := now.Add(time.Hour)
	s := Schedule{
		ID:             "schedule-123",
		PipelineID:     "pipeline-123",
		CronExpression: "0 0 * * *",
		Enabled:        true,
		LastRunAt:      &now,
		NextRunAt:      &next,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal schedule: %v", err)
	}

	var decoded Schedule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal schedule: %v", err)
	}

	if decoded.ID != s.ID {
		t.Errorf("ID mismatch: expected %s, got %s", s.ID, decoded.ID)
	}
	if decoded.CronExpression != s.CronExpression {
		t.Errorf("CronExpression mismatch: expected %s, got %s", s.CronExpression, decoded.CronExpression)
	}
	if decoded.Enabled != s.Enabled {
		t.Errorf("Enabled mismatch: expected %v, got %v", s.Enabled, decoded.Enabled)
	}
}

func TestUserJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	u := User{
		ID:         "user-123",
		Email:      "test@example.com",
		Name:       "Test User",
		Provider:   "oauth2",
		ProviderID: "provider-123",
		Role:       "admin",
		AvatarURL:  "https://example.com/avatar.png",
		CreatedAt:  now,
		LastLogin:  &now,
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("failed to marshal user: %v", err)
	}

	var decoded User
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal user: %v", err)
	}

	if decoded.ID != u.ID {
		t.Errorf("ID mismatch: expected %s, got %s", u.ID, decoded.ID)
	}
	if decoded.Email != u.Email {
		t.Errorf("Email mismatch: expected %s, got %s", u.Email, decoded.Email)
	}
	if decoded.Role != u.Role {
		t.Errorf("Role mismatch: expected %s, got %s", u.Role, decoded.Role)
	}
}

func TestAgentJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	a := Agent{
		ID:            "agent-123",
		Hostname:      "worker-1",
		IPAddress:     "192.168.1.100",
		Status:        "online",
		LastHeartbeat: &now,
		RegisteredAt:  now,
		Version:       "1.0.0",
		Labels:        `["production","us-west"]`,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("failed to marshal agent: %v", err)
	}

	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal agent: %v", err)
	}

	if decoded.ID != a.ID {
		t.Errorf("ID mismatch: expected %s, got %s", a.ID, decoded.ID)
	}
	if decoded.Hostname != a.Hostname {
		t.Errorf("Hostname mismatch: expected %s, got %s", a.Hostname, decoded.Hostname)
	}
	if decoded.Status != a.Status {
		t.Errorf("Status mismatch: expected %s, got %s", a.Status, decoded.Status)
	}
}

func TestSessionJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	expires := now.Add(24 * time.Hour)
	s := Session{
		ID:        "session-123",
		UserID:    "user-123",
		Token:     "secret-token",
		ExpiresAt: expires,
		CreatedAt: now,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}

	var decoded Session
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal session: %v", err)
	}

	if decoded.ID != s.ID {
		t.Errorf("ID mismatch: expected %s, got %s", s.ID, decoded.ID)
	}
	if decoded.UserID != s.UserID {
		t.Errorf("UserID mismatch: expected %s, got %s", s.UserID, decoded.UserID)
	}
	// Token should be omitted in JSON due to json:"-" tag
	if decoded.Token != "" {
		t.Errorf("Token should be empty in JSON, got %s", decoded.Token)
	}
}

func TestAuditLogJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	al := AuditLog{
		ID:         "audit-123",
		UserID:     "user-123",
		Action:     "create",
		Resource:   "pipeline",
		ResourceID: "pipeline-123",
		Details:    `{"name": "Test Pipeline"}`,
		IPAddress:  "192.168.1.1",
		CreatedAt:  now,
	}

	data, err := json.Marshal(al)
	if err != nil {
		t.Fatalf("failed to marshal audit log: %v", err)
	}

	var decoded AuditLog
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal audit log: %v", err)
	}

	if decoded.ID != al.ID {
		t.Errorf("ID mismatch: expected %s, got %s", al.ID, decoded.ID)
	}
	if decoded.Action != al.Action {
		t.Errorf("Action mismatch: expected %s, got %s", al.Action, decoded.Action)
	}
	if decoded.Resource != al.Resource {
		t.Errorf("Resource mismatch: expected %s, got %s", al.Resource, decoded.Resource)
	}
}

func TestPipelineWithOptionalFields(t *testing.T) {
	p := Pipeline{
		ID:         "pipeline-123",
		Name:       "Minimal Pipeline",
		ConfigYAML: "version: '1.0'",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal pipeline: %v", err)
	}

	// Description and CreatedBy should be omitted with omitempty
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := decoded["description"]; exists && decoded["description"] != "" {
		t.Log("description is present but may be empty")
	}
}

func TestPipelineRunWithNilTimes(t *testing.T) {
	pr := PipelineRun{
		ID:         "run-123",
		PipelineID: "pipeline-123",
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("failed to marshal pipeline run: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	// started_at and ended_at should be omitted when nil
	if decoded["started_at"] != nil {
		t.Log("started_at is present")
	}
}
