// Package sdk provides the Plugin SDK for Conduix.
// Plugin authors import this package and implement the Stage interface,
// then call sdk.Serve() in main() to start the plugin process.
package sdk

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "github.com/conduix/conduix/plugin-sdk/proto/plugin/v1"
)

// Handshake is the shared handshake config between host and plugin.
// Both sides must agree on these values.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "CONDUIX_PLUGIN",
	MagicCookieValue: "conduix-stage-plugin-v1",
}

// Record represents a single data record in the pipeline.
type Record struct {
	Data     map[string]any    `json:"data"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RecordFromProto converts a protobuf Record to an SDK Record.
func RecordFromProto(pr *pb.Record) (*Record, error) {
	r := &Record{
		Data:     make(map[string]any),
		Metadata: pr.Metadata,
	}
	if len(pr.Data) > 0 {
		if err := json.Unmarshal(pr.Data, &r.Data); err != nil {
			return nil, fmt.Errorf("unmarshal record data: %w", err)
		}
	}
	return r, nil
}

// ToProto converts an SDK Record to a protobuf Record.
func (r *Record) ToProto() (*pb.Record, error) {
	data, err := json.Marshal(r.Data)
	if err != nil {
		return nil, fmt.Errorf("marshal record data: %w", err)
	}
	return &pb.Record{
		Data:     data,
		Metadata: r.Metadata,
	}, nil
}

// Stage is the interface that plugin authors implement.
type Stage interface {
	// Init initializes the stage with JSON config from the pipeline definition.
	Init(config map[string]any) error

	// ProcessBatch processes a batch of records and returns the results.
	// Records can be modified, filtered out (by not including them), or new records added.
	ProcessBatch(records []*Record) ([]*Record, error)

	// Close gracefully shuts down the stage, releasing any resources.
	Close() error
}

// StagePluginImpl is the go-plugin implementation.
type StagePluginImpl struct {
	plugin.Plugin
	Stage Stage
}

// GRPCServer registers the gRPC server for the plugin side.
func (p *StagePluginImpl) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterStagePluginServer(s, &grpcServer{stage: p.Stage})
	return nil
}

// GRPCClient returns the gRPC client for the host side.
func (p *StagePluginImpl) GRPCClient(_ *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &GRPCClient{client: pb.NewStagePluginClient(c)}, nil
}

// Serve starts the plugin process and serves the Stage implementation.
// This should be the last call in main().
func Serve(stage Stage) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			"stage": &StagePluginImpl{Stage: stage},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

// ServeWithLogger starts the plugin process with a custom logger output.
func ServeWithLogger(stage Stage, logOutput *os.File) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			"stage": &StagePluginImpl{Stage: stage},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
