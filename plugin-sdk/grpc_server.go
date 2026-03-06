package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/conduix/conduix/plugin-sdk/proto/plugin/v1"
)

// grpcServer implements the protobuf StagePluginServer interface.
// It bridges gRPC calls to the user's Stage implementation.
type grpcServer struct {
	pb.UnimplementedStagePluginServer
	stage Stage
}

func (s *grpcServer) Init(_ context.Context, req *pb.InitRequest) (*pb.InitResponse, error) {
	var config map[string]any
	if len(req.Config) > 0 {
		if err := json.Unmarshal(req.Config, &config); err != nil {
			return &pb.InitResponse{Error: fmt.Sprintf("unmarshal config: %v", err)}, nil
		}
	}

	if err := s.stage.Init(config); err != nil {
		return &pb.InitResponse{Error: err.Error()}, nil
	}
	return &pb.InitResponse{}, nil
}

func (s *grpcServer) ProcessBatch(_ context.Context, req *pb.ProcessBatchRequest) (*pb.ProcessBatchResponse, error) {
	// Convert protobuf records to SDK records
	records := make([]*Record, 0, len(req.Records))
	for _, pr := range req.Records {
		r, err := RecordFromProto(pr)
		if err != nil {
			return &pb.ProcessBatchResponse{Error: err.Error()}, nil
		}
		records = append(records, r)
	}

	inputCount := len(records)

	// Call user's Stage implementation
	results, err := s.stage.ProcessBatch(records)
	if err != nil {
		return &pb.ProcessBatchResponse{Error: err.Error()}, nil
	}

	// Convert results back to protobuf
	pbRecords := make([]*pb.Record, 0, len(results))
	for _, r := range results {
		pr, err := r.ToProto()
		if err != nil {
			return &pb.ProcessBatchResponse{Error: err.Error()}, nil
		}
		pbRecords = append(pbRecords, pr)
	}

	return &pb.ProcessBatchResponse{
		Records:       pbRecords,
		FilteredCount: int32(inputCount - len(results)),
	}, nil
}

func (s *grpcServer) Close(_ context.Context, _ *pb.CloseRequest) (*pb.CloseResponse, error) {
	if err := s.stage.Close(); err != nil {
		return &pb.CloseResponse{Error: err.Error()}, nil
	}
	return &pb.CloseResponse{}, nil
}
