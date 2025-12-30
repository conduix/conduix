-- Conduix 초기 데이터베이스 설정

-- 데이터베이스 생성 (docker-compose에서 이미 생성되지만 안전을 위해)
CREATE DATABASE IF NOT EXISTS conduix CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE conduix;

-- 기본 관리자 사용자 생성 (선택적)
-- INSERT INTO users (id, email, name, role, created_at) VALUES
-- (UUID(), 'admin@example.com', 'Administrator', 'admin', NOW())
-- ON DUPLICATE KEY UPDATE id=id;

-- 인덱스 최적화
-- (GORM 마이그레이션에서 생성되지만 추가 인덱스가 필요한 경우)

-- 예시: 실행 히스토리 조회 최적화
-- CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_created
-- ON pipeline_runs (pipeline_id, created_at DESC);

-- 예시: 에이전트 하트비트 조회 최적화
-- CREATE INDEX IF NOT EXISTS idx_agents_status_heartbeat
-- ON agents (status, last_heartbeat);
