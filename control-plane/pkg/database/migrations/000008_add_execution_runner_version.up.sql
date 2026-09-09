-- WorkflowExecution.runner_version_id: 이 실행이 쓰는 native stage 바이너리 버전.
-- native stage 워크플로우는 GHCR 이미지가 아니라 runner_versions.Binary 로 실행되므로
-- (initContainer 가 CP 에서 받아 이미지의 바이너리를 덮어씀), 이 값이 없으면 "어떤 코드가
-- 돌고 있는지" 확인할 방법이 없다. 실측: 코어 수정 후 옛 바이너리로 계속 돌던 것을
-- kubectl 로 initContainer 명령을 뜯어야 알 수 있었다.
-- nullable — native stage 를 안 쓰는 워크플로우는 비어 있다.
-- 실제 스키마 반영은 GORM AutoMigrate 가 하며, 이 파일은 golang-migrate 경로 대비 보조.
ALTER TABLE workflow_executions ADD COLUMN runner_version_id VARCHAR(36) NULL
  COMMENT 'runner_versions.id — 이 실행이 실제로 쓰는 native 바이너리 버전';
CREATE INDEX idx_workflow_executions_runner_version ON workflow_executions (runner_version_id);
