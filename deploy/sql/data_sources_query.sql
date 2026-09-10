-- 연동 카탈로그 조회 모음

USE conduix;

-- 전체 요약
SELECT id, name, category, has_coordinates AS coord, total_records, status, verified_at
FROM data_sources ORDER BY category, id;

-- 특정 연동의 호출 방법 전체
-- SELECT * FROM data_sources WHERE id='public-restroom-info'\G

-- 연동에 필요한 키 (파이프라인 설정의 ${...} 이름 확인용)
SELECT s.id AS source, c.env_var, c.k8s_secret, c.valid_until
FROM data_sources s
JOIN data_source_credential_links l ON l.data_source_id = s.id
JOIN data_source_credentials c ON c.id = l.credential_id
ORDER BY s.id;

-- 만료 임박 키 (90일 내)
SELECT id, env_var, valid_until, DATEDIFF(valid_until, CURDATE()) AS days_left
FROM data_source_credentials
WHERE valid_until IS NOT NULL AND valid_until <= DATE_ADD(CURDATE(), INTERVAL 90 DAY)
ORDER BY valid_until;

-- 아직 검증 안 한 연동
SELECT id, name, status FROM data_sources WHERE verified_at IS NULL;

-- 좌표 없어 지오코딩이 필요한 수집 연동
SELECT id, name, target_table FROM data_sources
WHERE category='collect' AND has_coordinates=0;
