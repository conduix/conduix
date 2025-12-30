package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/stream"
)

func main() {
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	fmt.Println("=== Stream Pipeline Test ===")
	fmt.Println("Testing the new hybrid architecture:")
	fmt.Println("- Actor model for pipeline management")
	fmt.Println("- Direct function calls for data processing")
	fmt.Println()

	// Create Source (Demo source generates test data)
	source, err := stream.NewSource(stream.SourceConfig{
		Type: "demo",
		Name: "test-source",
		Config: map[string]any{
			"interval": "500ms", // Generate data every 500ms
		},
	})
	if err != nil {
		fmt.Printf("Failed to create source: %v\n", err)
		os.Exit(1)
	}

	// Create Stage chain
	// 1. Filter - only keep error and warn levels
	filter, err := stream.NewStage(stream.StageConfig{
		Type: "filter",
		Name: "level-filter",
		Config: map[string]any{
			"condition": ".level != \"debug\"",
		},
	})
	if err != nil {
		fmt.Printf("Failed to create filter: %v\n", err)
		os.Exit(1)
	}

	// 2. Remap - add processed timestamp
	remap, err := stream.NewStage(stream.StageConfig{
		Type:   "remap",
		Name:   "add-timestamp",
		Config: map[string]any{},
	})
	if err != nil {
		fmt.Printf("Failed to create remap: %v\n", err)
		os.Exit(1)
	}

	// 3. Enrich - add static fields
	enrich, err := stream.NewStage(stream.StageConfig{
		Type: "enrich",
		Name: "add-env",
		Config: map[string]any{
			"fields": map[string]any{
				"environment": "test",
				"pipeline":    "stream-test",
			},
		},
	})
	if err != nil {
		fmt.Printf("Failed to create enrich: %v\n", err)
		os.Exit(1)
	}

	stages := []stream.Stage{filter, remap, enrich}

	// Create Sink (Console output)
	sink, err := stream.NewSink(stream.SinkConfig{
		Type:   "console",
		Name:   "test-sink",
		Config: map[string]any{},
	})
	if err != nil {
		fmt.Printf("Failed to create sink: %v\n", err)
		os.Exit(1)
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(
		stream.ProcessorConfig{
			Name:       "stream-test-pipeline",
			BufferSize: 100,
			Logger:     logger,
		},
		source,
		stages,
		sink,
	)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the processor
	fmt.Println("Starting stream processor...")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	if err := processor.Start(ctx); err != nil {
		fmt.Printf("Failed to start processor: %v\n", err)
		os.Exit(1)
	}

	// Stats ticker
	statsTicker := time.NewTicker(3 * time.Second)
	defer statsTicker.Stop()

	// Run for 10 seconds or until signal
	timeout := time.After(10 * time.Second)

	for {
		select {
		case sig := <-sigCh:
			fmt.Printf("\nReceived signal: %v\n", sig)
			goto shutdown

		case <-timeout:
			fmt.Println("\nTest completed (10 second timeout)")
			goto shutdown

		case <-statsTicker.C:
			stats := processor.Stats()
			fmt.Printf("\n--- Stats ---\n")
			fmt.Printf("State: %s\n", processor.State())
			fmt.Printf("Input: %d, Output: %d, Filtered: %d, Errors: %d\n",
				stats.InputCount, stats.OutputCount, stats.FilteredCount, stats.ErrorCount)
			for name, ss := range stats.StageStats {
				fmt.Printf("  Stage %s: in=%d, out=%d, filtered=%d\n",
					name, ss.InputCount, ss.OutputCount, ss.FilteredCount)
			}
			fmt.Println()
		}
	}

shutdown:
	fmt.Println("\nStopping processor...")
	if err := processor.Stop(); err != nil {
		fmt.Printf("Error stopping processor: %v\n", err)
	}

	// Final stats
	stats := processor.Stats()
	fmt.Println("\n=== Final Stats ===")
	fmt.Printf("Total Input: %d\n", stats.InputCount)
	fmt.Printf("Total Output: %d\n", stats.OutputCount)
	fmt.Printf("Total Filtered: %d\n", stats.FilteredCount)
	fmt.Printf("Total Errors: %d\n", stats.ErrorCount)
	fmt.Printf("Processing Time: %v\n", stats.ProcessingTime)

	fmt.Println("\n=== Test Complete ===")
}
