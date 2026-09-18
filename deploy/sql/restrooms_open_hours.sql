-- 개방시간 정규화 컬럼.
--
-- 왜 필요한가: opn_hr_dtl 은 표기가 1,867 가지로 제각각이라(09:00~18:00 /
-- 0900-1800 / 09시~18시 / (평일)09:00~18:00 / 24시간 / 연중무휴 …) "지금 열렸나" 를
-- 물으려면 매번 사람이 정규식을 짜야 했다. 수집 시점에 정규화해 컬럼으로 박아두면
-- 화면·API 가 조건 한 줄로 판정할 수 있다.
--
-- 시간대: open_from/open_to 는 **KST 벽시계**다. "한국 기준 09:00 개방" 이라는
-- 현지 시각 자체가 의미이지 순간(instant)이 아니므로 UTC 로 환산하지 않는다.
-- DB 서버는 UTC 로 도니 판정 시 CONVERT_TZ(NOW(),'+00:00','+09:00') 로 맞춘다.
-- (시스템 타임스탬프인 synced_at 등은 UTC 유지 — 성격이 다르다.)
ALTER TABLE restrooms
  ADD COLUMN is_24h        tinyint(1)   NULL COMMENT '24시간/상시 개방',
  ADD COLUMN open_from     time         NULL COMMENT '개방 시작(KST 벽시계). is_24h 면 NULL',
  ADD COLUMN open_to       time         NULL COMMENT '개방 종료(KST 벽시계)',
  ADD COLUMN overnight     tinyint(1)   NULL COMMENT '자정 넘김(22:00~02:00 등)',
  ADD COLUMN weekday_only  tinyint(1)   NULL COMMENT '(평일)/월~금 표기',
  ADD COLUMN open_status   varchar(16)  NULL COMMENT '24h|ranged|closed|irregular|unknown',
  ADD COLUMN open_parse_src varchar(255) NULL COMMENT '파싱 근거 원문(검증용)';

-- "지금 열린 곳" 조회가 인덱스를 타게 한다.
CREATE INDEX idx_restrooms_open ON restrooms (open_status, is_24h, open_from, open_to);
