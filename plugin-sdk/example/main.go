// Example Conduix plugin that adds a risk flag based on score threshold.
//
// Build: go build -o example-plugin ./example
// This binary is executed by Pipeline Runner via HashiCorp go-plugin.
package main

import (
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type ScoreClassifier struct {
	threshold float64
	label     string
}

func (s *ScoreClassifier) Init(config map[string]any) error {
	s.threshold = 0.8
	if v, ok := config["threshold"].(float64); ok {
		s.threshold = v
	}
	s.label = "high"
	if v, ok := config["label"].(string); ok {
		s.label = v
	}
	return nil
}

func (s *ScoreClassifier) ProcessBatch(records []*sdk.Record) ([]*sdk.Record, error) {
	for _, r := range records {
		score, ok := r.Data["score"].(float64)
		if !ok {
			continue
		}
		if score >= s.threshold {
			r.Data["risk"] = s.label
		} else {
			r.Data["risk"] = "low"
		}
	}
	return records, nil
}

func (s *ScoreClassifier) Close() error {
	return nil
}

func main() {
	sdk.Serve(&ScoreClassifier{})
}
