package sdk

import (
	"testing"

	pb "github.com/conduix/conduix/plugin-sdk/proto/plugin/v1"
)

func TestRecordFromProto(t *testing.T) {
	pr := &pb.Record{
		Data:     []byte(`{"name":"alice","score":0.95}`),
		Metadata: map[string]string{"source": "kafka"},
	}

	r, err := RecordFromProto(pr)
	if err != nil {
		t.Fatalf("RecordFromProto: %v", err)
	}

	if r.Data["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", r.Data["name"])
	}
	if r.Data["score"] != 0.95 {
		t.Errorf("expected score=0.95, got %v", r.Data["score"])
	}
	if r.Metadata["source"] != "kafka" {
		t.Errorf("expected source=kafka, got %v", r.Metadata["source"])
	}
}

func TestRecordToProto(t *testing.T) {
	r := &Record{
		Data:     map[string]any{"id": float64(1), "value": "test"},
		Metadata: map[string]string{"key": "val"},
	}

	pr, err := r.ToProto()
	if err != nil {
		t.Fatalf("ToProto: %v", err)
	}

	if len(pr.Data) == 0 {
		t.Fatal("expected non-empty data")
	}
	if pr.Metadata["key"] != "val" {
		t.Errorf("expected key=val, got %v", pr.Metadata["key"])
	}
}

func TestRecordRoundTrip(t *testing.T) {
	original := &Record{
		Data: map[string]any{
			"name":   "bob",
			"score":  0.75,
			"active": true,
		},
		Metadata: map[string]string{"partition": "3"},
	}

	pr, err := original.ToProto()
	if err != nil {
		t.Fatalf("ToProto: %v", err)
	}

	restored, err := RecordFromProto(pr)
	if err != nil {
		t.Fatalf("RecordFromProto: %v", err)
	}

	if restored.Data["name"] != "bob" {
		t.Errorf("expected name=bob, got %v", restored.Data["name"])
	}
	if restored.Data["score"] != 0.75 {
		t.Errorf("expected score=0.75, got %v", restored.Data["score"])
	}
	if restored.Data["active"] != true {
		t.Errorf("expected active=true, got %v", restored.Data["active"])
	}
}

// testStage implements Stage for testing
type testStage struct {
	initConfig map[string]any
	closed     bool
}

func (s *testStage) Init(config map[string]any) error {
	s.initConfig = config
	return nil
}

func (s *testStage) ProcessBatch(records []*Record) ([]*Record, error) {
	for _, r := range records {
		r.Data["processed"] = true
	}
	return records, nil
}

func (s *testStage) Close() error {
	s.closed = true
	return nil
}

func TestGRPCServerProcessBatch(t *testing.T) {
	stage := &testStage{}
	server := &grpcServer{stage: stage}

	// Init
	initResp, err := server.Init(nil, &pb.InitRequest{
		Config: []byte(`{"key":"value"}`),
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if initResp.Error != "" {
		t.Fatalf("Init error: %s", initResp.Error)
	}
	if stage.initConfig["key"] != "value" {
		t.Errorf("expected config key=value, got %v", stage.initConfig["key"])
	}

	// ProcessBatch
	resp, err := server.ProcessBatch(nil, &pb.ProcessBatchRequest{
		Records: []*pb.Record{
			{Data: []byte(`{"id":1}`), Metadata: map[string]string{}},
			{Data: []byte(`{"id":2}`), Metadata: map[string]string{}},
		},
	})
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if len(resp.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(resp.Records))
	}

	// Close
	closeResp, err := server.Close(nil, &pb.CloseRequest{})
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closeResp.Error != "" {
		t.Fatalf("Close error: %s", closeResp.Error)
	}
	if !stage.closed {
		t.Error("expected stage to be closed")
	}
}

func TestGRPCServerFilterRecords(t *testing.T) {
	// Stage that filters out records with score < 0.5
	filterStage := &filterTestStage{}
	server := &grpcServer{stage: filterStage}

	resp, err := server.ProcessBatch(nil, &pb.ProcessBatchRequest{
		Records: []*pb.Record{
			{Data: []byte(`{"score":0.9}`)},
			{Data: []byte(`{"score":0.3}`)},
			{Data: []byte(`{"score":0.7}`)},
		},
	})
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if len(resp.Records) != 2 {
		t.Errorf("expected 2 records (filtered 1), got %d", len(resp.Records))
	}
	if resp.FilteredCount != 1 {
		t.Errorf("expected filtered_count=1, got %d", resp.FilteredCount)
	}
}

type filterTestStage struct{}

func (s *filterTestStage) Init(_ map[string]any) error { return nil }
func (s *filterTestStage) Close() error                { return nil }

func (s *filterTestStage) ProcessBatch(records []*Record) ([]*Record, error) {
	var result []*Record
	for _, r := range records {
		if score, ok := r.Data["score"].(float64); ok && score >= 0.5 {
			result = append(result, r)
		}
	}
	return result, nil
}
