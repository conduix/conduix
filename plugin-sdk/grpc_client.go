package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/conduix/conduix/plugin-sdk/proto/plugin/v1"
)

// GRPCClient is the host-side gRPC client that calls plugin processes.
// Used by Pipeline Runner to communicate with plugin binaries.
type GRPCClient struct {
	client pb.StagePluginClient
}

// Init calls the plugin's Init method with JSON config.
func (c *GRPCClient) Init(config map[string]any) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	resp, err := c.client.Init(context.Background(), &pb.InitRequest{Config: configBytes})
	if err != nil {
		return fmt.Errorf("grpc init: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("plugin init: %s", resp.Error)
	}
	return nil
}

// ProcessBatch calls the plugin's ProcessBatch method.
func (c *GRPCClient) ProcessBatch(records []*Record) ([]*Record, error) {
	pbRecords := make([]*pb.Record, 0, len(records))
	for _, r := range records {
		pr, err := r.ToProto()
		if err != nil {
			return nil, fmt.Errorf("marshal record: %w", err)
		}
		pbRecords = append(pbRecords, pr)
	}

	resp, err := c.client.ProcessBatch(context.Background(), &pb.ProcessBatchRequest{Records: pbRecords})
	if err != nil {
		return nil, fmt.Errorf("grpc process_batch: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("plugin process_batch: %s", resp.Error)
	}

	results := make([]*Record, 0, len(resp.Records))
	for _, pr := range resp.Records {
		r, err := RecordFromProto(pr)
		if err != nil {
			return nil, fmt.Errorf("unmarshal result: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

// Close calls the plugin's Close method.
func (c *GRPCClient) Close() error {
	resp, err := c.client.Close(context.Background(), &pb.CloseRequest{})
	if err != nil {
		return fmt.Errorf("grpc close: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("plugin close: %s", resp.Error)
	}
	return nil
}
