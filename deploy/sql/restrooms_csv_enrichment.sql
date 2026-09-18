-- CSV 배포본(localdata.go.kr)에만 있는 시설 상세 정보.
--
-- 왜 필요한가: 같은 화장실 데이터라도 API(15155058)와 CSV(15012892)의 항목이 다르다.
-- 관리번호 기준 53,574건(99.99%)이 겹치지만, CSV 에는 장애인용·어린이용 변기수와
-- 기저귀교환대/비상벨 "위치" 가 들어 있다. 복지맵에서 "휠체어로 갈 수 있는 화장실",
-- "아이 기저귀 갈 수 있는 곳" 을 답하려면 이 정보가 필요하다.
--
-- API 는 유무(Y/N)만 주고 CSV 는 위치까지 준다 — 서로 대체가 아니라 보강 관계다.
ALTER TABLE restrooms
  -- 변기 수: 규모와 접근성 판단의 근거 (실측 채움률 100%)
  ADD COLUMN male_toilet_cnt          int NULL COMMENT '남성용 대변기수',
  ADD COLUMN male_urinal_cnt          int NULL COMMENT '남성용 소변기수',
  ADD COLUMN male_disabled_toilet_cnt int NULL COMMENT '남성용 장애인 대변기수',
  ADD COLUMN male_disabled_urinal_cnt int NULL COMMENT '남성용 장애인 소변기수',
  ADD COLUMN male_child_toilet_cnt    int NULL COMMENT '남성용 어린이 대변기수',
  ADD COLUMN male_child_urinal_cnt    int NULL COMMENT '남성용 어린이 소변기수',
  ADD COLUMN female_toilet_cnt          int NULL COMMENT '여성용 대변기수',
  ADD COLUMN female_disabled_toilet_cnt int NULL COMMENT '여성용 장애인 대변기수',
  ADD COLUMN female_child_toilet_cnt    int NULL COMMENT '여성용 어린이 대변기수',
  -- 위치 정보: API 는 유무만 주므로 CSV 로만 알 수 있다
  ADD COLUMN diaper_place varchar(255) NULL COMMENT '기저귀교환대 장소',
  ADD COLUMN bell_place   varchar(255) NULL COMMENT '비상벨 설치장소',
  -- 시설 속성
  ADD COLUMN owner_type   varchar(60)  NULL COMMENT '화장실 소유구분',
  ADD COLUMN waste_type   varchar(60)  NULL COMMENT '오물처리방식',
  ADD COLUMN safety_target_yn varchar(4) NULL COMMENT '안전관리시설 설치대상 여부',
  ADD COLUMN law_basis    varchar(255) NULL COMMENT '근거법령명',
  ADD COLUMN install_ym   varchar(16)  NULL COMMENT '설치연월',
  ADD COLUMN remodel_ym   varchar(16)  NULL COMMENT '리모델링연월',
  -- 출처 추적: 어느 배포본에서 채웠는지 남긴다(재수집·정합성 확인용)
  ADD COLUMN csv_synced_at datetime(3) NULL COMMENT 'CSV 배포본 반영 시각';

-- 장애인/어린이 시설 보유 여부로 거르는 질의가 주 사용처다.
CREATE INDEX idx_restrooms_accessible
  ON restrooms (male_disabled_toilet_cnt, female_disabled_toilet_cnt);
