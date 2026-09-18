-- "지금 열린 화장실" 조회 — 정규화 컬럼만 보면 되므로 사람이 텍스트를 해석할 필요가 없다.
--
-- 시간대: open_from/open_to 는 KST 벽시계다. DB 서버는 UTC 로 돌기 때문에
-- CONVERT_TZ 로 맞춰 비교한다. 이 보정을 빠뜨리면 9시간 어긋난 판정이 나온다(실측).
--
-- weekday_only 는 (평일)/월~금 표기다. 주말에 그 시설을 열린 것으로 보고 싶지 않으면
-- 조건에 AND (weekday_only=0 OR DAYOFWEEK(...) BETWEEN 2 AND 6) 을 더한다.
SET @kst := TIME(CONVERT_TZ(NOW(),'+00:00','+09:00'));

SELECT mng_no, rstrm_nm, road_addr, open_status, open_from, open_to
  FROM restrooms
 WHERE is_24h = 1
    OR (open_status = 'ranged'
        AND (
          -- 자정을 넘기지 않는 보통의 운영
          (overnight = 0 AND @kst BETWEEN open_from AND open_to)
          -- 22:00~02:00 처럼 자정을 넘기는 운영
          OR (overnight = 1 AND (@kst >= open_from OR @kst < open_to))
        ))
 LIMIT 20;

-- 현황 요약
SELECT
  SUM(is_24h = 1) AS open_24h,
  SUM(open_status='ranged' AND overnight=0 AND @kst BETWEEN open_from AND open_to) AS open_now_ranged,
  SUM(open_status='ranged' AND overnight=1 AND (@kst >= open_from OR @kst < open_to)) AS open_now_overnight,
  SUM(open_status='unknown')   AS unknown,
  SUM(open_status='closed')    AS closed_always,
  SUM(open_status='irregular') AS irregular
FROM restrooms;
