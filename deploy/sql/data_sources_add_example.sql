-- 새 연동 추가 템플릿
--
-- 사용법: 이 파일을 복사해 값을 채우고 실행한다.
--   kubectl -n conduix exec -i mysql-cluster-0 -c mysql -- \
--     mysql -h127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" < my_new_source.sql
--
-- 채울 때 기준:
-- - params_desc / page_size_max / daily_quota: 몰라서 실패한 이력이 많다. 문서에 있으면 반드시 적는다.
-- - notes: 실측으로 알게 된 함정을 남긴다(빈값 존재, 필드 경로 차이, 좌표 없음 등).
--   여기 적어두지 않으면 다음 연동에서 같은 실수를 반복한다.
-- - verified_at: 실호출로 확인한 날짜만 적는다. 문서만 읽고 적지 않는다.

USE conduix;

-- 1) 키가 새로 필요하면 먼저 등록 (기존 키를 재사용하면 이 블록은 건너뛴다)
INSERT INTO data_source_credentials
  (id, env_var, secret_value, k8s_secret, provider, valid_from, valid_until, notes)
VALUES
  ('my-new-key', 'MY_NEW_KEY', 'REPLACE_ME', 'conduix-pipeline-secrets',
   '제공처', NULL, NULL, '용도 메모')
ON DUPLICATE KEY UPDATE secret_value=VALUES(secret_value), notes=VALUES(notes);

-- 2) 연동 등록
INSERT INTO data_sources
  (id, name, provider, dataset_no, category, base_url, http_method, auth_type, auth_param,
   response_format, data_field, params_desc, page_size_max, daily_quota, rate_limit_desc,
   total_records, has_coordinates, update_cycle, pk_strategy, description, notes,
   target_table, workflow_ids, status, verified_at)
VALUES
  ('my-new-source', '데이터셋 이름', '공공데이터포털', '데이터셋번호', 'collect',
   'https://api.data.go.kr/openapi/...', 'GET', 'query_param', 'serviceKey',
   'json', 'body.items.item',
   'serviceKey: 인증키(필수)\npageNo: 페이지 번호\nnumOfRows: 페이지당 건수(최대 ?)',
   1000, 10000, NULL,
   NULL, 1, '월', '원천 PK 전략',
   '무엇을 담은 데이터인지', '실측으로 알게 된 함정',
   'targetdb.my_table', NULL, 'planned', NULL)
ON DUPLICATE KEY UPDATE
  name=VALUES(name), base_url=VALUES(base_url), params_desc=VALUES(params_desc),
  notes=VALUES(notes), workflow_ids=VALUES(workflow_ids), verified_at=VALUES(verified_at);

-- 3) 연동 ↔ 키 연결
INSERT IGNORE INTO data_source_credential_links (data_source_id, credential_id) VALUES
  ('my-new-source', 'public-data-key');
