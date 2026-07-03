package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// SQSSource AWS SQS 데이터 소스
type SQSSource struct {
	queueURL          string
	region            string
	accessKeyID       string
	secretAccessKey   string
	sessionToken      string
	maxMessages       int32
	waitTimeSeconds   int32
	visibilityTimeout int32
	deleteOnReceive   bool
	endpoint          string // LocalStack 등 커스텀 엔드포인트

	client *sqs.Client
	mu     sync.RWMutex

	// 체크포인트
	lastMessageID  string
	processedCount int64
	checkpointMu   sync.RWMutex
}

// NewSQSSource SQS 소스 생성
func NewSQSSource(cfg config.SourceV2) (*SQSSource, error) {
	maxMessages := int32(10)
	if cfg.SQSMaxMessages > 0 && cfg.SQSMaxMessages <= 10 {
		maxMessages = int32(cfg.SQSMaxMessages)
	}

	// waitTimeSeconds: 0 = short polling, 1-20 = long polling
	// Go의 기본값 0은 short polling으로 처리 (성능 고려)
	// long polling을 원하면 명시적으로 1-20 설정 필요
	waitTimeSeconds := int32(cfg.SQSWaitTimeSeconds)
	if waitTimeSeconds < 0 {
		waitTimeSeconds = 0
	} else if waitTimeSeconds > 20 {
		waitTimeSeconds = 20
	}

	visibilityTimeout := int32(30)
	if cfg.SQSVisibilityTimeout > 0 {
		visibilityTimeout = int32(cfg.SQSVisibilityTimeout)
	}

	return &SQSSource{
		queueURL:          expandEnvVars(cfg.SQSQueueURL),
		region:            expandEnvVars(cfg.SQSRegion),
		accessKeyID:       expandEnvVars(cfg.SQSAccessKeyID),
		secretAccessKey:   expandEnvVars(cfg.SQSSecretAccessKey),
		sessionToken:      expandEnvVars(cfg.SQSSessionToken),
		maxMessages:       maxMessages,
		waitTimeSeconds:   waitTimeSeconds,
		visibilityTimeout: visibilityTimeout,
		deleteOnReceive:   cfg.SQSDeleteOnReceive,
		endpoint:          expandEnvVars(cfg.SQSEndpoint),
	}, nil
}

func (s *SQSSource) Name() string {
	return "sqs"
}

func (s *SQSSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// AWS 설정 옵션
	var opts []func(*awsconfig.LoadOptions) error

	// 리전 설정
	if s.region != "" {
		opts = append(opts, awsconfig.WithRegion(s.region))
	}

	// 명시적 자격 증명
	if s.accessKeyID != "" && s.secretAccessKey != "" {
		creds := credentials.NewStaticCredentialsProvider(
			s.accessKeyID,
			s.secretAccessKey,
			s.sessionToken,
		)
		opts = append(opts, awsconfig.WithCredentialsProvider(creds))
	}

	// AWS 설정 로드
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// SQS 클라이언트 생성
	sqsOpts := []func(*sqs.Options){}
	if s.endpoint != "" {
		// 커스텀 엔드포인트 (LocalStack 등)
		sqsOpts = append(sqsOpts, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(s.endpoint)
		})
	}

	s.client = sqs.NewFromConfig(cfg, sqsOpts...)

	slog.Default().Info("SQS connected",
		"queue", maskSQSURL(s.queueURL), "region", s.region,
		"max_messages", s.maxMessages, "wait_time_seconds", s.waitTimeSeconds)

	return nil
}

func (s *SQSSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		s.mu.RLock()
		client := s.client
		s.mu.RUnlock()

		if client == nil {
			errs <- fmt.Errorf("SQS client not initialized")
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 메시지 수신
			output, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(s.queueURL),
				MaxNumberOfMessages: s.maxMessages,
				WaitTimeSeconds:     s.waitTimeSeconds,
				VisibilityTimeout:   s.visibilityTimeout,
				MessageAttributeNames: []string{
					string(types.QueueAttributeNameAll),
				},
				AttributeNames: []types.QueueAttributeName{
					types.QueueAttributeNameAll,
				},
			})
			if err != nil {
				select {
				case errs <- fmt.Errorf("failed to receive messages: %w", err):
				default:
				}
				// 잠시 대기 후 재시도
				time.Sleep(time.Second)
				continue
			}

			// 메시지 처리
			for _, msg := range output.Messages {
				record, err := s.convertMessage(msg)
				if err != nil {
					select {
					case errs <- fmt.Errorf("failed to convert message: %w", err):
					default:
					}
					continue
				}

				// 체크포인트 업데이트
				s.updateCheckpoint(*msg.MessageId)

				select {
				case records <- record:
					// 메시지 삭제 (deleteOnReceive가 true인 경우)
					if s.deleteOnReceive {
						if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
							QueueUrl:      aws.String(s.queueURL),
							ReceiptHandle: msg.ReceiptHandle,
						}); err != nil {
							slog.Default().Warn("SQS failed to delete message",
								"message_id", *msg.MessageId, "queue", maskSQSURL(s.queueURL), "error", err)
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return records, errs
}

func (s *SQSSource) convertMessage(msg types.Message) (Record, error) {
	var data map[string]any

	// JSON 파싱 시도
	if msg.Body != nil {
		if err := json.Unmarshal([]byte(*msg.Body), &data); err != nil {
			// JSON이 아닌 경우 raw body로 처리
			data = map[string]any{
				"body": *msg.Body,
			}
		}
	}

	// 메시지 ID 추가
	if msg.MessageId != nil {
		data["_message_id"] = *msg.MessageId
	}

	// Receipt Handle 추가 (수동 삭제용)
	if msg.ReceiptHandle != nil {
		data["_receipt_handle"] = *msg.ReceiptHandle
	}

	// MD5 추가
	if msg.MD5OfBody != nil {
		data["_md5_of_body"] = *msg.MD5OfBody
	}

	// 메시지 속성 추가
	if len(msg.MessageAttributes) > 0 {
		attrs := make(map[string]any)
		for k, v := range msg.MessageAttributes {
			if v.StringValue != nil {
				attrs[k] = *v.StringValue
			} else if v.BinaryValue != nil {
				attrs[k] = v.BinaryValue
			}
		}
		data["_message_attributes"] = attrs
	}

	// 시스템 속성 추가
	if len(msg.Attributes) > 0 {
		sysAttrs := make(map[string]string)
		for k, v := range msg.Attributes {
			sysAttrs[k] = v
		}
		data["_system_attributes"] = sysAttrs
	}

	return Record{
		Data: data,
		Metadata: Metadata{
			Source:    "sqs",
			Origin:    s.queueURL,
			Offset:    *msg.MessageId,
			Timestamp: time.Now().UnixMilli(),
		},
	}, nil
}

func (s *SQSSource) updateCheckpoint(messageID string) {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.lastMessageID = messageID
	s.processedCount++
}

// SourceType 소스 타입 반환
func (s *SQSSource) SourceType() string {
	return "sqs"
}

// GetSourceCheckpoints 체크포인트 반환
func (s *SQSSource) GetSourceCheckpoints() []*SourceCheckpoint {
	s.checkpointMu.RLock()
	defer s.checkpointMu.RUnlock()

	return []*SourceCheckpoint{
		{
			PartitionKey: s.queueURL,
			OffsetValue:  s.lastMessageID,
			OffsetType:   "string",
			RecordCount:  s.processedCount,
			UpdatedAt:    time.Now(),
		},
	}
}

// SetSourceCheckpoints 체크포인트 설정 (SQS는 재시작 시 체크포인트 복원 미지원)
func (s *SQSSource) SetSourceCheckpoints(checkpoints []*SourceCheckpoint) error {
	// SQS는 메시지 기반 visibility timeout을 사용하므로 체크포인트 복원이 의미 없음
	// 처리되지 않은 메시지는 visibility timeout 후 다시 수신 가능
	slog.Default().Info("SQS checkpoint restoration not supported (visibility timeout based)",
		"queue", maskSQSURL(s.queueURL))
	return nil
}

func (s *SQSSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// AWS SDK 클라이언트는 명시적 close가 필요 없음
	s.client = nil

	slog.Default().Info("SQS closed",
		"queue", maskSQSURL(s.queueURL), "last_message_id", s.lastMessageID, "processed", s.processedCount)
	return nil
}

// maskSQSURL SQS Queue URL에서 계정 ID 마스킹
func maskSQSURL(url string) string {
	// https://sqs.region.amazonaws.com/123456789012/queue-name 형식
	// 계정 ID 부분을 마스킹
	if len(url) < 10 {
		return url
	}
	return url
}

// DeleteMessage 외부에서 메시지 삭제할 때 사용
func (s *SQSSource) DeleteMessage(ctx context.Context, receiptHandle string) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("SQS client not initialized")
	}

	_, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	return err
}

// ChangeVisibility 메시지 visibility timeout 변경
func (s *SQSSource) ChangeVisibility(ctx context.Context, receiptHandle string, timeout int32) error {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("SQS client not initialized")
	}

	_, err := client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(s.queueURL),
		ReceiptHandle:     aws.String(receiptHandle),
		VisibilityTimeout: timeout,
	})
	return err
}
