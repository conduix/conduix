// Package seed는 첫 실행 시 샘플 프로젝트/워크플로우를 기본 등록한다.
// 코어와 분리된 데모용이며, web-ui에서 삭제 가능하다.
// 샘플 프로젝트가 이미 존재하면 재시딩하지 않으므로, 사용자가 삭제한 샘플이 재생성되지 않는다.
package seed

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/conduix/conduix/control-plane/pkg/database"
	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

const (
	sampleProjectAlias = "samples"
	sampleProjectName  = "Sample Pipelines"
)

// endpoints는 샘플 파이프라인이 가리킬 소스/싱크 접속정보다.
// 기본값은 데모용 placeholder이며, 로컬 E2E에서는 SEED_* env로 mock 클러스터 DNS를 주입한다.
// (프로덕션은 env를 설정하지 않으므로 placeholder 그대로 — 동작 불변.)
type endpoints struct {
	restBaseURL string // REST 소스 base URL (예: http://mock-rest:8080)
	sourceDSN   string // 소스 MySQL DSN (mysqlInput용)
	targetDSN   string // 타깃 MySQL DSN (sqlOutput용)
	pgTargetDSN string // 타깃 PostgreSQL DSN
	kafkaBroker string // Kafka 브로커 주소
	cdcHost     string // MySQL CDC 소스 호스트
}

func loadEndpoints() endpoints {
	return endpoints{
		restBaseURL: envOr("SEED_MOCK_REST_URL", "https://api.example.com"),
		sourceDSN:   envOr("SEED_MOCK_SOURCE_DSN", "user:pass@tcp(localhost:3306)/sourcedb"),
		targetDSN:   envOr("SEED_MOCK_TARGET_DSN", "user:pass@tcp(localhost:3306)/targetdb"),
		pgTargetDSN: envOr("SEED_MOCK_PG_DSN", "postgres://user:pass@localhost:5432/targetdb?sslmode=disable"),
		kafkaBroker: envOr("SEED_MOCK_KAFKA_BROKER", "localhost:9092"),
		cdcHost:     envOr("SEED_MOCK_CDC_HOST", "localhost"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Run은 샘플 프로젝트가 없을 때만 샘플 프로젝트+워크플로우를 생성한다(멱등, 첫 실행 1회).
func Run(db *database.DB) error {
	var existing models.Project
	err := db.Where("alias = ?", sampleProjectAlias).First(&existing).Error
	if err == nil {
		return nil // 이미 시딩됨 — 재생성하지 않음(삭제한 샘플 부활 방지)
	}

	now := time.Now()
	project := &models.Project{
		ID:          uuid.New().String(),
		Name:        sampleProjectName,
		Alias:       sampleProjectAlias,
		Description: "설치 시 기본 등록되는 샘플 파이프라인 모음 (삭제 가능)",
		Status:      "active",
		Metadata:    `{"is_sample":true}`,
		CreatedBy:   "system",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(project).Error; err != nil {
		return fmt.Errorf("seed: create sample project: %w", err)
	}

	for _, w := range sampleWorkflows(project.ID) {
		if err := db.Create(w).Error; err != nil {
			slog.Warn("seed: failed to create sample workflow", "name", w.Name, "error", err)
			continue
		}
	}
	slog.Info("seeded sample project and workflows", "project", project.Alias)
	return nil
}

// newWorkflow는 샘플 워크플로우 모델을 만든다.
func newWorkflow(projectID, name, desc string, typ types.PipelineGroupType, pipelines []types.GroupedPipeline) *models.Workflow {
	for i := range pipelines {
		if pipelines[i].ID == "" {
			pipelines[i].ID = uuid.New().String()
		}
	}
	pj, _ := json.Marshal(pipelines)
	now := time.Now()
	return &models.Workflow{
		ID:              uuid.New().String(),
		ProjectID:       projectID,
		Name:            name,
		Description:     desc,
		Type:            string(typ),
		ExecutionMode:   string(types.ExecutionModeSequential),
		Status:          string(types.PipelineGroupStatusIdle),
		PipelinesConfig: string(pj),
		Metadata:        `{"is_sample":true}`,
		Tags:            `["sample"]`,
		CreatedBy:       "system",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// --- 커스텀(js_script) stage 샘플 — 빌드 없이 즉시 실행되는 실용 케이스 ---

// textToNumberStage: 문자열 필드를 숫자로 변환 (예: "42" → 42, "3.14" → 3.14).
func textToNumberStage() types.Stage {
	return types.Stage{
		Name: "text-to-number",
		Type: "js_script",
		Config: map[string]any{
			"code": `function process(record) {
  // 문자열로 들어온 숫자 필드를 실제 숫자로 변환
  if (typeof record.price === 'string') record.price = parseFloat(record.price);
  if (typeof record.quantity === 'string') record.quantity = parseInt(record.quantity, 10);
  return record;
}`,
			"timeout": "2s",
		},
	}
}

// jsonTransformStage: 필드를 재가공/합성 (예: full_name 생성, 상태 정규화).
func jsonTransformStage() types.Stage {
	return types.Stage{
		Name: "json-transform",
		Type: "js_script",
		Config: map[string]any{
			"code": `function process(record) {
  // 필드 합성/정규화
  if (record.first_name || record.last_name) {
    record.full_name = ((record.first_name || '') + ' ' + (record.last_name || '')).trim();
  }
  if (typeof record.status === 'string') record.status = record.status.toLowerCase();
  return record;
}`,
			"timeout": "2s",
		},
	}
}

// jsonExtractStage: 문자열로 저장된 JSON에서 중첩 필드 추출 (예: payload_json → user_email).
func jsonExtractStage() types.Stage {
	return types.Stage{
		Name: "json-extract",
		Type: "js_script",
		Config: map[string]any{
			"code": `function process(record) {
  // 문자열 JSON을 파싱해 중첩 필드를 상위로 추출
  if (typeof record.payload_json === 'string') {
    try {
      var p = JSON.parse(record.payload_json);
      if (p.user && p.user.email) record.user_email = p.user.email;
      if (p.event && p.event.type) record.event_type = p.event.type;
    } catch (e) {
      console.log('json-extract parse error: ' + e);
    }
  }
  return record;
}`,
			"timeout": "2s",
		},
	}
}

func mysqlInput(ep endpoints, table string) types.WorkflowInput {
	return types.WorkflowInput{
		Type: "sql", Name: "mysql-src",
		Config: map[string]any{
			"driver": "mysql",
			"dsn":    ep.sourceDSN,
			"query":  "SELECT * FROM " + table,
		},
	}
}

func restInput(url string) types.WorkflowInput {
	return types.WorkflowInput{
		Type: "rest_api", Name: "rest-src",
		Config: map[string]any{"url": url, "method": "GET"},
	}
}

func sqlOutput(name, driver, dsn, table string) types.Output {
	return types.Output{
		Name: name, Type: "sql",
		Config: map[string]any{"driver": driver, "dsn": dsn, "table": table},
	}
}

// sampleWorkflows는 요청된 6종 샘플(bulk 3 + cdc 3)을 커스텀 stage와 함께 구성한다.
// 접속정보는 endpoints(기본 placeholder, 로컬 E2E는 SEED_* env로 mock DNS 주입)에서 온다.
func sampleWorkflows(projectID string) []*models.Workflow {
	ep := loadEndpoints()
	return []*models.Workflow{
		// --- Bulk ---
		newWorkflow(projectID, "[bulk] MySQL → MySQL", "MySQL 소스를 커스텀 변환 후 MySQL에 적재", types.PipelineGroupTypeBatch,
			[]types.GroupedPipeline{{
				Name:    "mysql-to-mysql",
				Input:   mysqlInput(ep, "orders"),
				Stages:  []types.Stage{textToNumberStage(), jsonTransformStage()},
				Outputs: []types.Output{sqlOutput("mysql-sink", "mysql", ep.targetDSN, "orders_out")},
			}}),
		newWorkflow(projectID, "[bulk] REST → MySQL", "REST API를 폴링해 JSON 추출 후 MySQL 적재", types.PipelineGroupTypeBatch,
			[]types.GroupedPipeline{{
				Name:    "rest-to-mysql",
				Input:   restInput(ep.restBaseURL + "/orders"),
				Stages:  []types.Stage{jsonExtractStage(), textToNumberStage()},
				Outputs: []types.Output{sqlOutput("mysql-sink", "mysql", ep.targetDSN, "orders_out")},
			}}),
		newWorkflow(projectID, "[bulk] REST → PostgreSQL", "REST API를 폴링해 가공 후 PostgreSQL 적재", types.PipelineGroupTypeBatch,
			[]types.GroupedPipeline{{
				Name:    "rest-to-postgres",
				Input:   restInput(ep.restBaseURL + "/events"),
				Stages:  []types.Stage{jsonExtractStage(), jsonTransformStage()},
				Outputs: []types.Output{sqlOutput("pg-sink", "postgres", ep.pgTargetDSN, "events_out")},
			}}),

		// --- CDC / streaming ---
		newWorkflow(projectID, "[cdc] REST(polling) → MySQL", "REST 변경분을 폴링해 숫자 변환 후 MySQL 적재", types.PipelineGroupTypeRealtime,
			[]types.GroupedPipeline{{
				Name: "rest-cdc-to-mysql",
				Input: types.WorkflowInput{
					Type: "rest_api", Name: "rest-poll",
					Config: map[string]any{"url": ep.restBaseURL + "/changes", "method": "GET"},
				},
				Stages:  []types.Stage{jsonExtractStage(), textToNumberStage()},
				Outputs: []types.Output{sqlOutput("mysql-sink", "mysql", ep.targetDSN, "changes_out")},
			}}),
		newWorkflow(projectID, "[cdc] Kafka → MySQL", "Kafka CDC 이벤트를 가공/추출 후 MySQL 적재", types.PipelineGroupTypeRealtime,
			[]types.GroupedPipeline{{
				Name: "kafka-cdc-to-mysql",
				Input: types.WorkflowInput{
					Type: "kafka", Name: "kafka-src",
					Config: map[string]any{
						"brokers": []any{ep.kafkaBroker}, "topics": []any{"cdc.orders"}, "group_id": "conduix-sample",
					},
				},
				Stages:  []types.Stage{jsonExtractStage(), jsonTransformStage()},
				Outputs: []types.Output{sqlOutput("mysql-sink", "mysql", ep.targetDSN, "orders_cdc")},
			}}),
		newWorkflow(projectID, "[cdc] MySQL CDC → MySQL", "MySQL binlog CDC를 커스텀 변환 후 MySQL 적재", types.PipelineGroupTypeRealtime,
			[]types.GroupedPipeline{{
				Name: "mysql-cdc-to-mysql",
				Input: types.WorkflowInput{
					Type: "cdc", Name: "mysql-cdc",
					Config: map[string]any{
						"driver": "mysql", "host": ep.cdcHost, "port": 3306,
						"username": "root", "password": "rootpassword", "database": "sourcedb",
						"tables": []any{"orders"}, "server_id": 101,
					},
				},
				Stages:  []types.Stage{textToNumberStage(), jsonTransformStage()},
				Outputs: []types.Output{sqlOutput("mysql-sink", "mysql", ep.targetDSN, "orders_replica")},
			}}),
	}
}
