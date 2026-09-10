-- 외부 데이터 연동 정보 카탈로그 (개인 기록용, 로컬 DB)
--
-- 왜 필요한가: 연동한 API 의 엔드포인트·파라미터·인증키·수집 제약(일일 트래픽, 페이지
-- 상한)이 워크플로우 설정 JSON 안에만 흩어져 있었다. 새 연동을 추가할 때마다 기존 것을
-- 찾아 헤매고, 어떤 키가 어디에 쓰이는지 추적이 안 됐다. 여기 모아 한 곳에서 본다.
--
-- 키를 별도 테이블로 분리한 이유: API 설명·호출법은 자유롭게 조회하되 키는 따로 다루기
-- 위해서다. 로컬 개인 기록이므로 평문으로 둔다(사용자 결정).

CREATE DATABASE IF NOT EXISTS conduix
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE conduix;

CREATE TABLE IF NOT EXISTS data_sources (
  id              VARCHAR(64)  NOT NULL COMMENT '연동 식별자(kebab-case). 예: public-restroom-info',
  name            VARCHAR(200) NOT NULL COMMENT '사람이 읽는 이름',
  provider        VARCHAR(100) NOT NULL COMMENT '제공처. 예: 공공데이터포털, Kakao, Naver',
  dataset_no      VARCHAR(32)           COMMENT '공공데이터포털 데이터셋 번호(있으면)',
  category        VARCHAR(40)  NOT NULL DEFAULT 'collect'
                  COMMENT 'collect(수집 원천) | enrich(보강용. 지오코딩 등)',

  -- 호출 방법
  base_url        VARCHAR(500) NOT NULL COMMENT '엔드포인트(쿼리스트링 제외)',
  http_method     VARCHAR(10)  NOT NULL DEFAULT 'GET',
  auth_type       VARCHAR(40)  NOT NULL DEFAULT 'query_param'
                  COMMENT 'query_param | header | none',
  auth_param      VARCHAR(80)           COMMENT '인증 전달 위치. 예: serviceKey, Authorization',
  response_format VARCHAR(20)  NOT NULL DEFAULT 'json' COMMENT 'json | xml | json+xml',
  data_field      VARCHAR(200)          COMMENT '응답에서 레코드 배열 경로. 예: body.items.item',

  -- 파라미터·제약: 이 값들을 몰라 실패했던 이력이 많아 명시적으로 남긴다
  params_desc     TEXT                  COMMENT '주요 쿼리 파라미터 설명(줄바꿈 구분)',
  page_size_max   INT                   COMMENT '한 페이지 최대 건수(초과 시 에러)',
  daily_quota     INT                   COMMENT '일일 호출 허용량(건수 아님 — 호출 횟수)',
  rate_limit_desc VARCHAR(200)          COMMENT '초당/동시 요청 제한 등',

  -- 데이터 특성
  total_records   INT                   COMMENT '실측 전체 건수',
  has_coordinates TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '원천이 위경도를 제공하는가',
  update_cycle    VARCHAR(200)          COMMENT '갱신 주기(제공처 명시 + 실측). 실측 근거를 담아 길어진다',
  pk_strategy     VARCHAR(300)          COMMENT '식별 전략. 원천에 단일 PK 가 없는 경우가 많다',

  -- 운영 메모
  description     TEXT                  COMMENT '무엇을 담은 데이터인지',
  notes           TEXT                  COMMENT '함정·실측 결과·주의사항',
  target_table    VARCHAR(120)          COMMENT '적재 대상 테이블',
  workflow_ids    VARCHAR(500)          COMMENT '이 연동을 쓰는 워크플로우 id(콤마 구분)',
  status          VARCHAR(20)  NOT NULL DEFAULT 'active'
                  COMMENT 'active | planned | deprecated',
  verified_at     DATE                  COMMENT '실호출로 검증한 날짜',

  created_at      DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at      DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

  PRIMARY KEY (id),
  KEY idx_provider (provider),
  KEY idx_category (category),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='외부 데이터 연동 카탈로그. 새 연동 추가 시 여기에 한 행 추가';

CREATE TABLE IF NOT EXISTS data_source_credentials (
  id            VARCHAR(64)  NOT NULL COMMENT '자격 식별자. 예: public-data-key',
  env_var       VARCHAR(80)  NOT NULL COMMENT '파이프라인 설정에서 ${...} 로 참조하는 이름',
  secret_value  VARCHAR(500) NOT NULL COMMENT '키 값(로컬 개인 기록이라 평문)',
  k8s_secret    VARCHAR(120)          COMMENT '실제 주입되는 K8s Secret 이름',
  provider      VARCHAR(100) NOT NULL,
  valid_from    DATE                  COMMENT '활용 시작일',
  valid_until   DATE                  COMMENT '활용 종료일 — 만료 전 갱신 필요',
  notes         TEXT,
  created_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at    DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_env_var (env_var)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='연동 인증키. env_var 로 파이프라인 설정과 연결된다';

-- 연동 ↔ 자격 다대다. 하나의 키를 여러 연동이 공유한다(PUBLIC_DATA_KEY 가 그렇다).
CREATE TABLE IF NOT EXISTS data_source_credential_links (
  data_source_id VARCHAR(64) NOT NULL,
  credential_id  VARCHAR(64) NOT NULL,
  PRIMARY KEY (data_source_id, credential_id),
  KEY idx_credential (credential_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='연동과 인증키 연결';
