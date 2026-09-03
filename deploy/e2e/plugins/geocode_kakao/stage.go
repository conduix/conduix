// geocode_kakao — 주소 → 좌표 커스텀 stage (Plugin V3 / compile-in)
//
// 이 파일이 정본이며, 실제 배포는 plugins.SourceCode 메타데이터로 등록된다
// (POST /api/v1/plugins, name=geocode_kakao → stage type "geocode_kakao").
// 알고리즘 출처: docs/공중화장실-지오코딩-실패케이스.md
//
// 원칙 (문서와 동일):
//   - 비싼 호출은 마지막에: 정규화 → 영속 캐시(geocode_cache) → 파손 판정 → API.
//     캐시가 상태의 본체 — 이미 좌표가 있는 주소는 API 를 부르지 않고,
//     주소가 바뀌면 addr_norm 이 달라져 캐시 미스 → 자동 재지오코딩.
//   - 근사 좌표는 성공이 아니다: 번지 특정 실패는 not_found (동 중심점 금지).
//   - 실패해도 레코드는 흘려보낸다 — 어댑터가 에러를 묻으므로(원본 passthrough)
//     에러 대신 geo_status 마커 필드로 표현한다.
//   - 한반도 범위 밖 좌표는 오매칭으로 간주하고 버린다.
//
// 플러그인 계약 주의:
//   - struct 이름은 반드시 Stage (registry_custom.go 가 &Stage{} 로 생성)
//   - context 없음 → http.Client Timeout 으로 자체 관리
//   - Process 는 병렬 호출됨 → 내부 상태는 전부 mutex/atomic 보호
//   - MySQL 드라이버는 runner 바이너리(pipeline-core SQL sink)에 이미 링크되어
//     있어 database/sql(표준) 만 import 하면 된다 — allowed_modules 등록 불필요
package geocode_kakao

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/conduix/conduix/plugin-sdk"
)

// 한반도 좌표 범위 — 실측: 동명 지역 오매칭으로 27km 오차 사례
const (
	koreaLatMin, koreaLatMax = 33.0, 39.6
	koreaLonMin, koreaLonMax = 124.5, 132.0
)

// geocode_cache.status 와 동일한 어휘
const (
	statusOK        = "ok"
	statusNotFound  = "not_found"
	statusUnfixable = "unfixable"
)

type geoResult struct {
	Status    string
	Lat, Lon  float64
	Provider  string
	MatchType string // road | region
}

// Stage — 이름 고정 계약 (빌더가 &Stage{} 생성 후 Init(config) 호출)
type Stage struct {
	sdk.BaseNativeStage

	apiKey       string
	apiBaseURL   string
	addressField string
	lotnoField   string
	maxRetries   int
	dailyQuota   int64
	cacheTable   string

	client  *http.Client
	db      *sql.DB
	minGap  time.Duration // 호출 간 최소 간격 (rps 의 역수)
	paceMu  sync.Mutex
	paceAt  time.Time
	memMu   sync.RWMutex
	mem     map[string]*geoResult // 실행 내 핫 캐시 (본체는 SQL)
	memCap  int
	flights sync.Map // addr_norm → *flight : 동시 동일 주소 중복 호출 방지
	quota   atomic.Int64
	calls   atomic.Int64
}

type flight struct {
	done chan struct{}
	res  *geoResult
}

func cfgStr(c map[string]any, key, def string) string {
	if v, ok := c[key].(string); ok && v != "" {
		return v
	}
	return def
}

func cfgNum(c map[string]any, key string, def float64) float64 {
	switch v := c[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func (s *Stage) Init(config map[string]any) error {
	s.apiKey = cfgStr(config, "api_key", "") // 비우면 캐시 전용 모드 (API 미호출)
	s.apiBaseURL = strings.TrimRight(cfgStr(config, "api_base_url", "https://dapi.kakao.com"), "/")
	s.addressField = cfgStr(config, "address_field", "")
	if s.addressField == "" {
		return fmt.Errorf("geocode_kakao: address_field is required")
	}
	s.lotnoField = cfgStr(config, "lotno_field", "")
	s.maxRetries = int(cfgNum(config, "max_retries", 3))
	s.dailyQuota = int64(cfgNum(config, "daily_quota", 100000))
	rps := cfgNum(config, "rps", 20)
	if rps <= 0 {
		rps = 20
	}
	s.minGap = time.Duration(float64(time.Second) / rps)
	s.memCap = int(cfgNum(config, "memory_cache_size", 50000))
	s.mem = make(map[string]*geoResult, 1024)
	s.client = &http.Client{Timeout: 10 * time.Second}

	if dsn := cfgStr(config, "cache_dsn", ""); dsn != "" {
		s.cacheTable = cfgStr(config, "cache_table", "geocode_cache")
		db, err := sql.Open(cfgStr(config, "cache_driver", "mysql"), dsn)
		if err != nil {
			return fmt.Errorf("geocode_kakao: open cache db: %w", err)
		}
		db.SetMaxOpenConns(4)
		s.db = db
	}
	return nil
}

func (s *Stage) Close() error {
	slog.Default().Info("[geocode_kakao] closed", "api_calls", s.calls.Load())
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Stage) Process(record map[string]any) (map[string]any, error) {
	addrRaw, _ := record[s.addressField].(string)
	lotnoRaw := ""
	if s.lotnoField != "" {
		lotnoRaw, _ = record[s.lotnoField].(string)
	}
	if strings.TrimSpace(addrRaw) == "" {
		addrRaw = lotnoRaw
	}
	norm := normalizeAddress(addrRaw)
	if norm == "" {
		record["geo_status"] = "no_address"
		return record, nil
	}

	res := s.resolve(norm, lotnoRaw)

	// geo_addr = 이 결과가 어떤 주소 기준인지의 기록 — 주소 변경 감지의 앵커
	record["geo_addr"] = norm
	record["geo_status"] = res.Status
	if res.Status == statusOK {
		record["lat"] = res.Lat
		record["lon"] = res.Lon
		record["geo_source"] = res.Provider
		record["geo_match"] = res.MatchType
	}
	// 실패 시 lat/lon 미설정 → sink upsert 가 NULL 로 반영.
	// 주소가 바뀌어 재실패한 좌표는 옛 위치가 틀린 것이므로 지우는 게 맞다.
	return record, nil
}

// resolve LRU → SQL 캐시 → 파손 판정 → API(+폴백). 동시 동일 주소는 1회만 호출.
func (s *Stage) resolve(norm, lotnoRaw string) *geoResult {
	if r := s.memGet(norm); r != nil {
		return r
	}

	f := &flight{done: make(chan struct{})}
	if actual, loaded := s.flights.LoadOrStore(norm, f); loaded {
		af := actual.(*flight)
		<-af.done
		if af.res != nil {
			return af.res
		}
		return &geoResult{Status: "error"}
	}
	defer func() {
		close(f.done)
		s.flights.Delete(norm)
	}()

	if r := s.cacheGet(norm); r != nil {
		s.memSet(norm, r)
		f.res = r
		return r
	}
	if isUnfixableAddress(norm) {
		r := &geoResult{Status: statusUnfixable}
		s.cachePut(norm, r)
		s.memSet(norm, r)
		f.res = r
		return r
	}
	if s.apiKey == "" { // 캐시 전용 모드 — 확정 아님이므로 캐시에 남기지 않는다
		r := &geoResult{Status: "no_api_key"}
		f.res = r
		return r
	}

	r := s.geocodeWithFallbacks(norm, lotnoRaw)
	if r.Status == statusOK || r.Status == statusNotFound {
		s.cachePut(norm, r)
		s.memSet(norm, r)
	}
	f.res = r
	return r
}

// geocodeWithFallbacks 후보를 순서대로 — 먼저 성공하면 중단 (정확한 방법부터).
// 폴백: spacing 양방향 / 지번 재시도 / 번지표기 정규화 / 부번 탐색 / 행정구역 보강.
func (s *Stage) geocodeWithFallbacks(norm, lotnoRaw string) *geoResult {
	candidates := []string{norm}
	candidates = append(candidates, spacingVariants(norm)...)
	if ln := normalizeAddress(lotnoRaw); ln != "" && ln != norm {
		candidates = append(candidates, ln)
	}
	if v := lotNotationVariant(norm); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, sublotVariants(norm)...)

	tried := map[string]bool{}
	for _, cand := range candidates {
		if cand == "" || tried[cand] {
			continue
		}
		tried[cand] = true
		r, stop := s.query(cand)
		if r != nil {
			return r
		}
		if stop {
			return &geoResult{Status: "error"}
		}
	}

	// 행정구역 보강: '가좌4동 399' → '가좌4동' 조회로 시도/시군구 얻어 재시도
	if enriched := s.regionPrefixCandidate(norm); enriched != "" && !tried[enriched] {
		if r, _ := s.query(enriched); r != nil {
			return r
		}
	}
	return &geoResult{Status: statusNotFound}
}

// query 카카오 주소검색 1회 (간격 제한·429 백오프·쿼터·범위 검증).
// 반환 (결과, 중단) — 결과 nil + 중단 false = 미매칭, 다음 후보 진행.
func (s *Stage) query(q string) (*geoResult, bool) {
	if s.quota.Load() >= s.dailyQuota {
		slog.Default().Warn("[geocode_kakao] daily quota exhausted — 남은 주소는 다음 실행이 이어간다")
		return nil, true
	}
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		s.pace()
		s.quota.Add(1)
		s.calls.Add(1)

		doc, code, err := s.kakaoSearch(q)
		switch {
		case err != nil:
			return nil, true
		case code == http.StatusTooManyRequests || code >= 500:
			if attempt >= s.maxRetries {
				return nil, true
			}
			time.Sleep(backoff)
			backoff *= 2
			continue
		case code == http.StatusUnauthorized || code == http.StatusForbidden:
			slog.Default().Error("[geocode_kakao] API key rejected", "status", code)
			return nil, true
		case code != http.StatusOK:
			return nil, true
		}
		if doc == nil {
			return nil, false
		}
		if doc.lat < koreaLatMin || doc.lat > koreaLatMax || doc.lon < koreaLonMin || doc.lon > koreaLonMax {
			return nil, false // 범위 밖 = 오매칭 — 근사·오답 좌표는 쓰지 않는다
		}
		return &geoResult{Status: statusOK, Lat: doc.lat, Lon: doc.lon, Provider: "kakao", MatchType: doc.matchType}, false
	}
}

// pace 호출 간 최소 간격 강제 (초당 요청 제한 — 에러 23 대응)
func (s *Stage) pace() {
	s.paceMu.Lock()
	now := time.Now()
	if s.paceAt.After(now) {
		wait := s.paceAt.Sub(now)
		s.paceAt = s.paceAt.Add(s.minGap)
		s.paceMu.Unlock()
		time.Sleep(wait)
		return
	}
	s.paceAt = now.Add(s.minGap)
	s.paceMu.Unlock()
}

type kakaoDoc struct {
	lat, lon           float64
	matchType          string
	region1, region2   string
}

func (s *Stage) kakaoSearch(q string) (*kakaoDoc, int, error) {
	u := s.apiBaseURL + "/v2/local/search/address.json?size=1&query=" + url.QueryEscape(q)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "KakaoAK "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var body struct {
		Documents []struct {
			X           string `json:"x"`
			Y           string `json:"y"`
			RoadAddress *struct{ X, Y string } `json:"road_address"`
			Address     *struct {
				X, Y             string
				Region1DepthName string `json:"region_1depth_name"`
				Region2DepthName string `json:"region_2depth_name"`
			} `json:"address"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body.Documents) == 0 {
		return nil, http.StatusOK, nil
	}
	d := body.Documents[0]
	doc := &kakaoDoc{matchType: "region"}
	if d.RoadAddress != nil {
		doc.matchType = "road"
	}
	doc.lon, _ = strconv.ParseFloat(d.X, 64)
	doc.lat, _ = strconv.ParseFloat(d.Y, 64)
	if d.Address != nil {
		doc.region1 = d.Address.Region1DepthName
		doc.region2 = d.Address.Region2DepthName
	}
	return doc, http.StatusOK, nil
}

func (s *Stage) regionPrefixCandidate(norm string) string {
	parts := strings.SplitN(norm, " ", 2)
	if len(parts) != 2 || !reDongEnding.MatchString(parts[0]) {
		return ""
	}
	doc, code, err := s.kakaoSearch(parts[0])
	if err != nil || code != http.StatusOK || doc == nil || doc.region1 == "" {
		return ""
	}
	return strings.TrimSpace(doc.region1 + " " + doc.region2 + " " + norm)
}

// --- 메모리 캐시 (본체는 SQL — 이건 실행 내 핫 캐시) ---

func (s *Stage) memGet(k string) *geoResult {
	s.memMu.RLock()
	defer s.memMu.RUnlock()
	return s.mem[k]
}

func (s *Stage) memSet(k string, r *geoResult) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	if len(s.mem) >= s.memCap { // 용량 초과 시 전체 리셋 (배치 실행 수명이라 단순하게)
		s.mem = make(map[string]*geoResult, 1024)
	}
	s.mem[k] = r
}

// --- 영속 캐시 (geocode_cache — 기존 python 산출물과 스키마 공유) ---

func (s *Stage) cacheGet(norm string) *geoResult {
	if s.db == nil {
		return nil
	}
	q := fmt.Sprintf("SELECT status, IFNULL(lat,0), IFNULL(lon,0), IFNULL(provider,''), IFNULL(match_type,'') FROM %s WHERE addr_norm = ?", s.cacheTable)
	r := &geoResult{}
	err := s.db.QueryRow(q, norm).Scan(&r.Status, &r.Lat, &r.Lon, &r.Provider, &r.MatchType)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Default().Warn("[geocode_kakao] cache read failed", "error", err)
		}
		return nil
	}
	return r
}

func (s *Stage) cachePut(norm string, r *geoResult) {
	if s.db == nil {
		return
	}
	q := fmt.Sprintf(`INSERT INTO %s (addr_norm, lat, lon, provider, match_type, status, attempts)
		VALUES (?, ?, ?, ?, ?, ?, 1)
		ON DUPLICATE KEY UPDATE lat=VALUES(lat), lon=VALUES(lon), provider=VALUES(provider),
		match_type=VALUES(match_type), status=VALUES(status), attempts=attempts+1`, s.cacheTable)
	var lat, lon, provider, match any
	if r.Status == statusOK {
		lat, lon, provider, match = r.Lat, r.Lon, r.Provider, r.MatchType
	}
	if _, err := s.db.Exec(q, norm, lat, lon, provider, match, r.Status); err != nil {
		slog.Default().Warn("[geocode_kakao] cache write failed", "error", err)
	}
}

// --- 주소 정규화·판정·변형 (문서의 규칙 — 실측 기여도는 문서 참조) ---

var (
	reParenPair    = regexp.MustCompile(`\([^)]*\)`)
	reMultiSpace   = regexp.MustCompile(`\s{2,}`)
	reBungilAttach = regexp.MustCompile(`(\d+번길)\s+(\d)`)
	reBungilDetach = regexp.MustCompile(`(\d+번길)(\d)`)
	reRoadDetach   = regexp.MustCompile(`([가-힣](?:로|길))(\d)`)
	reLotHo        = regexp.MustCompile(`(산\s*)?(\d+)번지\s*(\d+)호`)
	reLotOnly      = regexp.MustCompile(`(산\s*)?(\d+)번지`)
	reTrailingNum  = regexp.MustCompile(`^(.*\s)(\d+)$`)
	reDongEnding   = regexp.MustCompile(`(동|리|가)\d*$|(\d+동)$`)
	reDescriptive  = regexp.MustCompile(`(인근|부근|일대|앞|옆|뒤|해안가|마을|입구|주변|밑|내)$`)
	reMultiLot     = regexp.MustCompile(`외\s*\d*\s*필지`)
	reNoSpaceLong  = regexp.MustCompile(`^\S{10,}$`)
	reSidoPrefix   = regexp.MustCompile(`^(서울|부산|대구|인천|광주|대전|울산|세종|경기|강원|충청|충북|충남|전라|전북|전남|경상|경북|경남|제주)`)
)

// normalizeAddress 지오코더를 방해하는 표기만 제거 (쉼표 절단·괄호쌍 제거·번길 분리·공백 정리)
func normalizeAddress(addr string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}
	if i := strings.Index(a, ","); i >= 0 {
		a = a[:i]
	}
	a = reParenPair.ReplaceAllString(a, " ") // 안 닫힌 괄호는 파손 판정으로 넘긴다
	a = reBungilDetach.ReplaceAllString(a, "$1 $2")
	a = reMultiSpace.ReplaceAllString(a, " ")
	return strings.TrimSpace(a)
}

// isUnfixableAddress 지오코더를 바꿔도 실패하는 주소 (보수적으로 — 애매하면 시도한다)
func isUnfixableAddress(norm string) bool {
	return reDescriptive.MatchString(norm) ||
		!reSidoPrefix.MatchString(norm) ||
		reNoSpaceLong.MatchString(norm) ||
		reMultiLot.MatchString(norm) ||
		(strings.Contains(norm, "(") && !strings.Contains(norm, ")"))
}

func spacingVariants(norm string) []string {
	var out []string
	if v := reBungilAttach.ReplaceAllString(norm, "$1$2"); v != norm {
		out = append(out, v)
	}
	if v := reRoadDetach.ReplaceAllString(norm, "$1 $2"); v != norm {
		out = append(out, v)
	}
	return out
}

func lotNotationVariant(norm string) string {
	v := reLotHo.ReplaceAllStringFunc(norm, func(m string) string {
		g := reLotHo.FindStringSubmatch(m)
		san := ""
		if strings.TrimSpace(g[1]) != "" {
			san = "산"
		}
		return san + g[2] + "-" + g[3]
	})
	if v == norm {
		v = reLotOnly.ReplaceAllStringFunc(norm, func(m string) string {
			g := reLotOnly.FindStringSubmatch(m)
			san := ""
			if strings.TrimSpace(g[1]) != "" {
				san = "산"
			}
			return san + g[2]
		})
	}
	if v == norm {
		return ""
	}
	return v
}

// sublotVariants 번지는 미등록이어도 부번은 등록된 경우 — 'N' → 'N-1' ~ 'N-5'
func sublotVariants(norm string) []string {
	g := reTrailingNum.FindStringSubmatch(norm)
	if g == nil || strings.Contains(g[2], "-") {
		return nil
	}
	out := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		out = append(out, fmt.Sprintf("%s%s-%d", g[1], g[2], i))
	}
	return out
}
