package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
)

// PipelineLinkService Pipeline Link Management Service
// Manages parent-child pipeline connections and automatic Kafka topic creation/deletion
type PipelineLinkService struct {
	db          *database.DB
	kafka       *KafkaService
	logger      *slog.Logger
	autoCleanup bool // Auto cleanup links when no children
}

// NewPipelineLinkService creates PipelineLinkService
func NewPipelineLinkService(db *database.DB, kafka *KafkaService, logger *slog.Logger) *PipelineLinkService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PipelineLinkService{
		db:          db,
		kafka:       kafka,
		logger:      logger,
		autoCleanup: true,
	}
}

// CreateLink creates pipeline link
// Automatically creates Kafka Topic and saves link info to DB
// Note: Kafka brokers are NOT stored - they are fetched from environment at runtime
func (s *PipelineLinkService) CreateLink(ctx context.Context, workflowID, parentPipelineID, childPipelineID, createdBy string) (*models.PipelineLink, error) {
	// Find workflow
	var workflow models.Workflow
	if err := s.db.First(&workflow, "id = ?", workflowID).Error; err != nil {
		return nil, fmt.Errorf("workflow not found: %w", err)
	}

	// Parse PipelinesConfig to extract pipeline names
	var pipelines []map[string]any
	if err := json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines); err != nil {
		return nil, fmt.Errorf("failed to parse pipelines config: %w", err)
	}

	var parentName, childName string
	for _, p := range pipelines {
		if id, ok := p["id"].(string); ok {
			if id == parentPipelineID {
				if name, ok := p["name"].(string); ok {
					parentName = name
				}
			}
			if id == childPipelineID {
				if name, ok := p["name"].(string); ok {
					childName = name
				}
			}
		}
	}

	if parentName == "" || childName == "" {
		return nil, fmt.Errorf("parent or child pipeline not found in workflow")
	}

	// Check if link already exists
	var existingLink models.PipelineLink
	err := s.db.Where("parent_pipeline_id = ? AND child_pipeline_id = ?", parentPipelineID, childPipelineID).First(&existingLink).Error
	if err == nil {
		return &existingLink, nil // Return existing link
	}
	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("failed to check existing link: %w", err)
	}

	// Generate Kafka Topic name
	topicName := s.kafka.GenerateTopicName(workflow.Slug, parentName, childName)

	// Create Kafka Topic
	if err := s.kafka.CreateTopic(ctx, topicName); err != nil {
		return nil, fmt.Errorf("failed to create kafka topic: %w", err)
	}

	// Create link (Kafka Brokers NOT stored - fetched from environment at runtime)
	link := &models.PipelineLink{
		ID:               uuid.New().String(),
		WorkflowID:       workflowID,
		ParentPipelineID: parentPipelineID,
		ChildPipelineID:  childPipelineID,
		KafkaTopic:       topicName,
		Status:           "active",
		CreatedBy:        createdBy,
	}

	if err := s.db.Create(link).Error; err != nil {
		// Rollback topic on link creation failure
		if cleanupErr := s.kafka.DeleteTopic(ctx, topicName); cleanupErr != nil {
			s.logger.Error("Failed to cleanup kafka topic after link creation failure",
				"topic", topicName, "error", cleanupErr)
		}
		return nil, fmt.Errorf("failed to create link: %w", err)
	}

	s.logger.Info("Pipeline link created",
		"link_id", link.ID,
		"parent", parentPipelineID,
		"child", childPipelineID,
		"topic", topicName)

	return link, nil
}

// DeleteLink deletes pipeline link
// Automatically deletes Kafka Topic
func (s *PipelineLinkService) DeleteLink(ctx context.Context, parentPipelineID, childPipelineID string) error {
	// Find link
	var link models.PipelineLink
	if err := s.db.Where("parent_pipeline_id = ? AND child_pipeline_id = ?", parentPipelineID, childPipelineID).First(&link).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to find link: %w", err)
	}

	// Delete Kafka Topic
	if err := s.kafka.DeleteTopic(ctx, link.KafkaTopic); err != nil {
		s.logger.Error("Failed to delete kafka topic, continuing with link deletion",
			"topic", link.KafkaTopic, "error", err)
	}

	// Delete link
	if err := s.db.Delete(&link).Error; err != nil {
		return fmt.Errorf("failed to delete link: %w", err)
	}

	s.logger.Info("Pipeline link deleted",
		"link_id", link.ID,
		"parent", parentPipelineID,
		"child", childPipelineID,
		"topic", link.KafkaTopic)

	return nil
}

// GetLinksByParent gets all links for parent pipeline
func (s *PipelineLinkService) GetLinksByParent(ctx context.Context, parentPipelineID string) ([]models.PipelineLink, error) {
	var links []models.PipelineLink
	if err := s.db.Where("parent_pipeline_id = ?", parentPipelineID).Find(&links).Error; err != nil {
		return nil, fmt.Errorf("failed to find links: %w", err)
	}
	return links, nil
}

// GetLinksByChild gets all links for child pipeline
func (s *PipelineLinkService) GetLinksByChild(ctx context.Context, childPipelineID string) ([]models.PipelineLink, error) {
	var links []models.PipelineLink
	if err := s.db.Where("child_pipeline_id = ?", childPipelineID).Find(&links).Error; err != nil {
		return nil, fmt.Errorf("failed to find links: %w", err)
	}
	return links, nil
}

// GetLinksByWorkflow gets all links for workflow
func (s *PipelineLinkService) GetLinksByWorkflow(ctx context.Context, workflowID string) ([]models.PipelineLink, error) {
	var links []models.PipelineLink
	if err := s.db.Where("workflow_id = ?", workflowID).Find(&links).Error; err != nil {
		return nil, fmt.Errorf("failed to find links: %w", err)
	}
	return links, nil
}

// CleanupOrphanedLinks auto cleanup links when no children
// This method is called periodically to cleanup unused links
func (s *PipelineLinkService) CleanupOrphanedLinks(ctx context.Context) error {
	if !s.autoCleanup {
		return nil
	}

	// Get all links
	var links []models.PipelineLink
	if err := s.db.Find(&links).Error; err != nil {
		return fmt.Errorf("failed to fetch links: %w", err)
	}

	// Group by parent
	parentLinks := make(map[string][]models.PipelineLink)
	for _, link := range links {
		parentLinks[link.ParentPipelineID] = append(parentLinks[link.ParentPipelineID], link)
	}

	// Delete links when parent has no children
	for parentID, parentLinksList := range parentLinks {
		hasActiveChildren := false
		for _, link := range parentLinksList {
			// Check if child pipeline still exists in workflow
			var workflow models.Workflow
			if err := s.db.First(&workflow, "id = ?", link.WorkflowID).Error; err != nil {
				s.logger.Warn("Workflow not found for link", "workflow_id", link.WorkflowID, "link_id", link.ID)
				continue
			}

			var pipelines []map[string]any
			if err := json.Unmarshal([]byte(workflow.PipelinesConfig), &pipelines); err != nil {
				s.logger.Warn("Failed to parse pipelines config", "workflow_id", workflow.ID, "error", err)
				continue
			}

			for _, p := range pipelines {
				if id, ok := p["id"].(string); ok && id == link.ChildPipelineID {
					hasActiveChildren = true
					break
				}
			}

			if hasActiveChildren {
				break
			}
		}

		// Delete all links if no active children
		if !hasActiveChildren {
			for _, link := range parentLinksList {
				if err := s.DeleteLink(ctx, link.ParentPipelineID, link.ChildPipelineID); err != nil {
					s.logger.Error("Failed to cleanup orphaned link",
						"link_id", link.ID,
						"parent", parentID,
						"error", err)
				}
			}
		}
	}

	return nil
}

// GetLink gets specific link
func (s *PipelineLinkService) GetLink(ctx context.Context, parentPipelineID, childPipelineID string) (*models.PipelineLink, error) {
	var link models.PipelineLink
	if err := s.db.Where("parent_pipeline_id = ? AND child_pipeline_id = ?", parentPipelineID, childPipelineID).First(&link).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find link: %w", err)
	}
	return &link, nil
}
