-- Plugin config_schema: 커스텀 stage 의 GUI 설정 폼 자동생성용 스키마(types.StageSchema JSON).
-- nullable — 스키마 미등록 플러그인도 목록에는 뜨고 폼은 JSON 폴백으로 동작한다.
-- 실제 스키마 반영은 GORM AutoMigrate 가 하며, 이 파일은 golang-migrate 경로 대비 보조.
ALTER TABLE plugins ADD COLUMN config_schema JSON NULL
  COMMENT 'types.StageSchema 직렬화 — GUI 설정 폼 생성용';
