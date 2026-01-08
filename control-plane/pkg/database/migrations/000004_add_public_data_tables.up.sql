-- 공공API 데이터 저장용 테이블

-- 아산시 주정차단속 위치 정보
CREATE TABLE IF NOT EXISTS asan_parking_enforcement (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    seq_no INT COMMENT '연번',
    enforcement_type VARCHAR(50) COMMENT '단속유형',
    install_address VARCHAR(500) COMMENT '설치주소',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_enforcement_type (enforcement_type),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='아산시 주정차단속 위치 정보';

-- 공공API 수집 로그
CREATE TABLE IF NOT EXISTS public_api_collection_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    api_name VARCHAR(100) NOT NULL COMMENT 'API 이름',
    api_url VARCHAR(500) COMMENT 'API URL',
    total_count INT DEFAULT 0 COMMENT '총 데이터 수',
    collected_count INT DEFAULT 0 COMMENT '수집된 데이터 수',
    status ENUM('running', 'completed', 'failed') DEFAULT 'running',
    error_message TEXT,
    started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP NULL,
    INDEX idx_api_name (api_name),
    INDEX idx_status (status),
    INDEX idx_started_at (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='공공API 수집 로그';
