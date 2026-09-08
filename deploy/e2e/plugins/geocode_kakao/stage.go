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
	// overrideField: 원본 주소가 틀렸을 때 사람이 채워 넣는 보정 주소 컬럼.
	// 수집 sink 의 columns 에서 제외해 두면 재수집에도 값이 보존된다.
	overrideField string
	// placeNameField: 주소로 못 찾을 때 시설명으로 장소 검색할 컬럼(비면 미사용).
	placeNameField string
	maxRetries     int
	dailyQuota     int64
	cacheTable     string

	// skipIfGeocoded: 이미 이 주소로 좌표가 채워진 레코드는 재지오코딩하지 않는다.
	// realtime CDC 재사용 시 (1) 무변경 UPDATE 재처리 방지 (2) 지오코딩→lat/lon UPDATE 가
	// 다시 CDC 이벤트로 돌아와도 재지오코딩 안 함(무한루프 차단). 기본 켜짐.
	skipIfGeocoded bool

	// 네이버(NCP Maps) 폴백 — 카카오가 못 찾은 주소만 재시도(문서 알고리즘 S4).
	// 키(id/secret) 둘 다 있어야 활성. 카카오 실패 후에만 호출해 쿼터를 아낀다.
	naverID     string
	naverSecret string
	naverURL    string
	naverQuota  int64
	naverUsed   atomic.Int64
	naverCalls  atomic.Int64

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

func cfgBool(c map[string]any, key string, def bool) bool {
	switch v := c[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
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
	s.overrideField = cfgStr(config, "override_field", "")
	s.placeNameField = cfgStr(config, "place_name_field", "")
	s.skipIfGeocoded = cfgBool(config, "skip_if_geocoded", true)
	// 네이버 폴백: id/secret 둘 다 있어야 켜짐. 검증된 신형 엔드포인트가 기본값.
	s.naverID = cfgStr(config, "naver_client_id", "")
	s.naverSecret = cfgStr(config, "naver_client_secret", "")
	s.naverURL = strings.TrimRight(cfgStr(config, "naver_base_url", "https://maps.apigw.ntruss.com"), "/")
	s.naverQuota = int64(cfgNum(config, "naver_daily_quota", 3000))
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
	slog.Default().Info("[geocode_kakao] closed", "kakao_calls", s.calls.Load(), "naver_calls", s.naverCalls.Load())
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

	// 보정 주소(override)가 있으면 원본 대신 그것으로 지오코딩한다.
	// 원본(road_addr)은 수집 파이프라인이 매번 덮어쓰므로 오타를 고쳐도 다음 수집에서
	// 원복된다(실측: 창원시 표기 정정 95건이 배치 재실행으로 전부 되돌아갔다).
	// 보정 컬럼을 수집 sink 의 columns 에서 빼두면 upsert 가 건드리지 않아 사람이 고친 값이
	// 살아남는다 — lat/lon 이 보존되는 것과 같은 원리다.
	// 비어 있으면 미사용 → 원본이 정상인 레코드는 아무 설정 없이 그대로 동작한다.
	if s.overrideField != "" {
		if ov, _ := record[s.overrideField].(string); strings.TrimSpace(ov) != "" {
			addrRaw = ov
		}
	}

	norm := normalizeAddress(addrRaw)
	if norm == "" {
		record["geo_status"] = "no_address"
		return record, nil
	}

	// CDC delete 는 좌표 계산 없이 통과(sink 가 PK 로 삭제 처리).
	if t, _ := record["_cdc_type"].(string); t == "delete" {
		return record, nil
	}

	// 게이트: 이미 이 주소(geo_addr)로 좌표가 채워진 레코드면 재지오코딩하지 않고 드롭한다.
	// "주소 안 바뀜 AND 좌표 있음" 둘 다여야 드롭 — 주소는 그대로여도 좌표가 비어 들어오면
	// (이전 지오코딩 실패분, 또는 batch 가 좌표 없이 수집한 신규분) 다시 지오코딩해 채운다.
	// nil 반환 = 레코드 드롭(sink 미전송). record 를 통과시키면 CDC after 의 (그 시점) lat 이
	// sink 로 되쓰여 방금 지오코딩한 좌표를 옛값으로 되돌리는 race 가 생긴다. 무변경이므로 드롭이 옳다.
	// 이 게이트가 곧 무한루프 차단: 지오코딩→lat/lon UPDATE→그 CDC 이벤트는 geo_addr==norm 이고
	// lat 이 차 있어 여기서 드롭되어 재지오코딩·재기록이 없다.
	if s.skipIfGeocoded {
		lat, hasLat := record["lat"]
		prev, _ := record["geo_addr"].(string)
		if hasLat && lat != nil && prev == norm {
			return nil, nil
		}
	}

	res := s.resolve(norm, lotnoRaw)

	// 시설명 폴백(S5): 주소로는 어떤 지오코더도 못 찾을 때 시설명으로 장소를 검색한다.
	// 원본 주소 자체가 틀린 경우를 구제한다 — 실측: '삼정자로48번길 3'(원본)은 두 API 모두
	// 실패하지만 시설명 '성주빌딩' 으로 검색하면 실제 주소 '삼정자로43번길 8' 이 나온다.
	//
	// 주소 캐시(resolve) 밖에서 처리하는 이유: 캐시 키가 addr_norm 이라 시설명 결과를 넣으면
	// 같은 주소를 공유하는 다른 시설이 그 좌표를 물려받는다. 실측으로 '대전광역시 서구' 한
	// 주소에 시설명이 97개, '동작동' 에 22개 있어 오염 규모가 크다.
	//
	// 오매칭 위험이 커서(시설명 '성주빌딩' 단독 검색은 89건) 검증을 통과해야 채택한다.
	if res.Status != statusOK && s.placeNameField != "" {
		if pn, _ := record[s.placeNameField].(string); pn != "" {
			if r := s.placeNameFallback(pn, norm); r != nil {
				res = r
			}
		}
	}

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
		// road 가 파손·근사불가여도 lotno(지번주소)가 온전하면 그걸로 구제한다.
		// road 만 보고 unfixable 로 확정하면, 도로명이 깨진 레코드가 지번주소를 놔두고 버려진다.
		if ln := normalizeAddress(lotnoRaw); ln != "" && ln != norm && !isUnfixableAddress(ln) && s.apiKey != "" {
			if r, _ := s.query(ln); r != nil {
				s.cachePut(norm, r)
				s.memSet(norm, r)
				f.res = r
				return r
			}
		}
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

	// 네이버 폴백(S4): 카카오가 모든 후보로 못 찾은 주소만 네이버로 재시도.
	// 카카오 DB 에만 없는 주소를 보강한다(실측: 카카오 not_found 를 네이버가 다수 복구).
	//
	// 카카오와 같은 후보 목록을 쓴다. 예전에는 norm(도로명) 하나만 넘겨서, 원본 도로명 번지가
	// 틀렸고 지번주소로는 찾히는 레코드를 놓쳤다 — 실측: '서천군 서면 요포길 123'(원본, 실제는
	// 135번지)은 실패하지만 지번 '서면 도둔리 1222-33' 으로는 네이버가 찾는다.
	// 좌표 없는 810건 중 348건이 지번주소를 갖고 있고 표본 20건에서 14건이 회복됐다.
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if r := s.naverGeocode(cand); r != nil {
			return r
		}
	}

	return &geoResult{Status: statusNotFound}
}

// naverGeocode NCP Maps Geocoding 폴백. 키 없으면 nil(스킵), 쿼터 소진 시 nil.
// 카카오와 응답 스키마가 다르다(addresses[].roadAddress/jibunAddress/x/y).
// placeNameFallback 은 시설명으로 카카오 장소검색(keyword)을 호출해 좌표를 얻는다.
// 주소가 틀려 어떤 지오코더도 못 찾는 레코드의 마지막 수단이다.
//
// 오매칭을 막는 검증 3중:
//  1. 쿼리에 원본 주소의 시/도+시군구를 붙여 검색 범위를 좁힌다('성주빌딩' 단독은 89건).
//  2. 반환 place_name 이 원본 시설명과 접두 일치율 0.7 이상이어야 한다. 표본 14건에서
//     정상 10건은 0.70~1.00, 오매칭 4건('현내리마을회관'→'수통1리마을회관' 0.00,
//     '한국의원'→'한국흉부외과의원' 0.50)은 전부 0.5 이하로 갈렸다.
//     전체 유사도(SequenceMatcher)로는 정상 0.67 과 오매칭 0.67 이 겹쳐 분리되지 않는다.
//  3. 이름이 3자 미만이면 신뢰하지 않는다('시장' → '정선아리랑시장' 류 오매칭 차단).
func (s *Stage) placeNameFallback(placeName, addrNorm string) *geoResult {
	if s.apiKey == "" {
		return nil
	}
	name := normalizePlaceName(placeName)
	if len([]rune(name)) < 3 {
		return nil
	}
	region := regionTokens(addrNorm)
	if region == "" {
		return nil // 지역 한정 없이 검색하면 동명 시설 오매칭 위험이 크다
	}

	s.pace()
	s.calls.Add(1)
	u := s.apiBaseURL + "/v2/local/search/keyword.json?query=" + url.QueryEscape(region+" "+name)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "KakaoAK "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Documents []struct {
			PlaceName       string `json:"place_name"`
			AddressName     string `json:"address_name"`
			RoadAddressName string `json:"road_address_name"`
			X               string `json:"x"`
			Y               string `json:"y"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Documents) == 0 {
		return nil
	}

	d := body.Documents[0]
	if prefixMatchRatio(name, normalizePlaceName(d.PlaceName)) < placeNameMinRatio {
		return nil
	}
	// 반환 주소가 원본과 같은 시군구인지 — 지역 한정을 넣었어도 API 가 넓게 잡을 수 있다.
	if sig := sigunguOf(addrNorm); sig != "" && !strings.Contains(d.AddressName, sig) &&
		!strings.Contains(d.RoadAddressName, sig) {
		return nil
	}

	// 읍/면/동까지 대조한다. 시군구만 보면 같은 군 안의 다른 읍면을 통과시킨다 —
	// 실측 오매칭: '금강삼사' 원본 '고성군 현내면 화포리 561-1' 인데 반환은
	// '고성군 거진읍 화진포길 204-25'. 같은 고성군이고 시설명도 정확히 일치해
	// 접두·시군구 검증을 모두 통과했다.
	//
	// 원본에 읍면동이 없으면(시군구까지만 기재된 주소) 이 검증은 건너뛴다 — 대조할
	// 근거가 없고, 그런 레코드는 애초에 주소검색이 불가해 시설명이 유일한 단서다.
	if emd := eupMyeonDongOf(addrNorm); emd != "" &&
		!strings.Contains(d.AddressName, emd) && !strings.Contains(d.RoadAddressName, emd) {
		return nil
	}

	lon, _ := strconv.ParseFloat(d.X, 64)
	lat, _ := strconv.ParseFloat(d.Y, 64)
	if lat < koreaLatMin || lat > koreaLatMax || lon < koreaLonMin || lon > koreaLonMax {
		return nil
	}
	return &geoResult{Status: statusOK, Lat: lat, Lon: lon, Provider: "kakao_place", MatchType: "place"}
}

// placeNameMinRatio 는 시설명 접두 일치율 하한. 표본 14건 실측으로 정한 값(정상 0.70~1.00 /
// 오매칭 0.00~0.50). 낮추면 오매칭이 통과하고, 높이면 '국립서울현충원 유공자'(0.70) 류를 놓친다.
const placeNameMinRatio = 0.7

// normalizePlaceName 은 비교용으로 시설명을 정리한다 — 괄호 부가설명, '화장실' 접미어,
// 공백·구분자를 제거한다('검단(서울)졸음쉼터' → '검단졸음쉼터').
func normalizePlaceName(s string) string {
	s = reParenPair.ReplaceAllString(s, "")
	s = reRestroomSuffix.ReplaceAllString(s, "")
	return reNameNoise.ReplaceAllString(s, "")
}

// prefixMatchRatio 는 a 의 앞부분이 b 와 연속으로 몇 비율 일치하는지 반환한다.
// 전체 유사도가 아니라 접두를 보는 이유: 오매칭은 앞부분(고유명)부터 다르고, 정상 매칭은
// 뒤에 수식어가 붙는 형태('중부대학교' → '중부대학교 국제캠퍼스')다.
func prefixMatchRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return 0
	}
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	return float64(i) / float64(len(ra))
}

// regionTokens 는 주소 앞부분에서 검색 범위 한정에 쓸 '시도 시군구' 를 뽑는다.
func regionTokens(addrNorm string) string {
	toks := strings.Fields(addrNorm)
	var out []string
	for _, t := range toks {
		if len(out) == 2 {
			break
		}
		if knownSido[t] || reSigunguHead.MatchString(t+" ") {
			out = append(out, t)
			continue
		}
		if len(out) > 0 {
			break // 시도 뒤 시군구가 아니면 중단
		}
	}
	return strings.Join(out, " ")
}

// eupMyeonDongOf 는 주소에서 읍/면/동 토큰을 뽑는다(반환 주소 검증용).
// '리' 는 제외한다 — 카카오 도로명 주소(road_address_name)에는 리가 나타나지 않아
// 대조하면 정상 매칭까지 탈락한다. 읍면동까지가 실용적 상한이다.
// 숫자가 붙은 행정동('면목3,8동', '양평2동')도 그대로 비교한다 — 반환 주소도 같은 표기를 쓴다.
func eupMyeonDongOf(addrNorm string) string {
	for _, t := range strings.Fields(addrNorm) {
		if knownSido[t] || reSigunguHead.MatchString(t+" ") {
			continue
		}
		if reEupMyeonDong.MatchString(t) {
			return t
		}
	}
	return ""
}

// sigunguOf 는 주소에서 시/군/구 토큰 하나를 뽑는다(반환 주소 검증용).
func sigunguOf(addrNorm string) string {
	for _, t := range strings.Fields(addrNorm) {
		if knownSido[t] {
			continue
		}
		if reSigunguHead.MatchString(t + " ") {
			return t
		}
	}
	return ""
}

func (s *Stage) naverGeocode(q string) *geoResult {
	if s.naverID == "" || s.naverSecret == "" {
		return nil
	}
	if s.naverUsed.Load() >= s.naverQuota {
		slog.Default().Warn("[geocode_kakao] naver quota exhausted")
		return nil
	}
	s.pace()
	s.naverUsed.Add(1)
	s.naverCalls.Add(1)

	u := s.naverURL + "/map-geocode/v2/geocode?query=" + url.QueryEscape(q)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("x-ncp-apigw-api-key-id", s.naverID)
	req.Header.Set("x-ncp-apigw-api-key", s.naverSecret)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			slog.Default().Error("[geocode_kakao] naver key rejected", "status", resp.StatusCode)
		}
		return nil
	}
	var body struct {
		Addresses []struct {
			RoadAddress  string `json:"roadAddress"`
			JibunAddress string `json:"jibunAddress"`
			X            string `json:"x"` // 경도
			Y            string `json:"y"` // 위도
			// addressElements 로 "번지까지 특정됐는지" 를 판정한다. 네이버는 동 이름만 줘도
			// (예: '서울특별시 동작구 동작동') 좌표를 반환하는데 그건 동 중심점이라 시설 위치가
			// 아니다 — "근사 좌표는 성공이 아니다" 원칙에 따라 거부해야 한다.
			AddressElements []struct {
				Types    []string `json:"types"`
				LongName string   `json:"longName"`
			} `json:"addressElements"`
		} `json:"addresses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Addresses) == 0 {
		return nil
	}
	a := body.Addresses[0]

	// 번지(LAND_NUMBER) 또는 건물번호(BUILDING_NUMBER) 중 하나라도 있어야 지점이 특정된 것.
	// 둘 다 비면 시/군/구/동 레벨 매칭 = 중심점이므로 버린다.
	pinpointed := false
	for _, el := range a.AddressElements {
		if el.LongName == "" {
			continue
		}
		for _, ty := range el.Types {
			if ty == "LAND_NUMBER" || ty == "BUILDING_NUMBER" {
				pinpointed = true
			}
		}
	}
	if !pinpointed {
		return nil
	}

	lon, _ := strconv.ParseFloat(a.X, 64)
	lat, _ := strconv.ParseFloat(a.Y, 64)
	if lat < koreaLatMin || lat > koreaLatMax || lon < koreaLonMin || lon > koreaLonMax {
		return nil // 범위 밖 = 오매칭
	}
	matchType := "region"
	if a.RoadAddress != "" {
		matchType = "road"
	}
	return &geoResult{Status: statusOK, Lat: lat, Lon: lon, Provider: "naver", MatchType: matchType}
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
	lat, lon         float64
	matchType        string
	region1, region2 string
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
			X           string                 `json:"x"`
			Y           string                 `json:"y"`
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
	reParenPair      = regexp.MustCompile(`\([^)]*\)`)
	reMultiSpace     = regexp.MustCompile(`\s{2,}`)
	reBungilAttach   = regexp.MustCompile(`(\d+번길)\s+(\d)`)
	reBungilDetach   = regexp.MustCompile(`(\d+번길)(\d)`)
	reRoadDetach     = regexp.MustCompile(`([가-힣](?:로|길))(\d)`)
	reLotHo          = regexp.MustCompile(`(산\s*)?(\d+)번지\s*(\d+)호`)
	reLotOnly        = regexp.MustCompile(`(산\s*)?(\d+)번지`)
	reTrailingNum    = regexp.MustCompile(`^(.*\s)(\d+)$`)
	reDongEnding     = regexp.MustCompile(`(동|리|가)\d*$|(\d+동)$`)
	reDescriptive    = regexp.MustCompile(`(인근|부근|일대|앞|옆|뒤|해안가|마을|입구|주변|밑|내)$`)
	reMultiLot       = regexp.MustCompile(`외\s*\d*\s*필지`)
	reRestroomSuffix = regexp.MustCompile(`\s*(공중)?화장실.*$`)
	reNameNoise      = regexp.MustCompile(`[\s\-_,·]`)
	reEupMyeonDong   = regexp.MustCompile(`^[가-힣][가-힣0-9,]*(읍|면|동)$`)
	reSidoPrefix     = regexp.MustCompile(`^(서울|부산|대구|인천|광주|대전|울산|세종|경기|강원|충청|충북|충남|전라|전북|전남|경상|경북|경남|제주)`)
	// reSigunguHead: 시도 접두를 뗀 뒤 맨 앞이 시/군/구인지 — API 가 시도를 판별할 근거가 남았는지 확인용
	reSigunguHead = regexp.MustCompile(`^[가-힣]+(시|군|구)(\s|$)`)
	// 도로명(로/길 뒤 번호) 또는 지번(동/리/가 뒤 번지) 형태 — sido 접두가 없어도 주소로 보고 시도.
	// 아래 네 형태를 모두 인정해야 한다. 각각 실측으로 확인한 누락 사례가 있다(2026-09-08):
	//   - `\s?\d*(로|길)`: '공단 7로 31' — 로/길 앞이 숫자면 기존 패턴이 탈락시켰다. 실제
	//     도로명은 '공단7로' 인데 원본 데이터에 공백이 섞여 들어온다(228건).
	//   - `(동|리|가)\s*\d`: '구호동239' — 공백을 필수(\s+)로 요구해 붙어 쓴 번지를 놓쳤다(583건).
	//   - `산\s*\d+`: '달방동 산 3-6' — 산번지 형태가 아예 없었다. 최대 누락 덩어리(1,226건).
	//   - `\d+-\d+$`: '현내면 757-5' — 동/리 없이 번지만 남은 지번.
	//   - `(지하\s*)?`: '영등포구 경인로 지하 843' — 지하상가 주소. 로/길 뒤에 '지하' 가
	//     끼어들어 번호가 바로 오지 않아 탈락했다. API 는 '지하' 를 포함해도 정상 처리한다(실측).
	// 번지 없는 도로명('강릉시 남부로')은 의도적으로 제외한다 — 카카오가 address_type=ROAD 로
	// 도로 전체의 대표 좌표를 주고 후보가 10건씩 나와, 화장실 위치가 아닌 좌표가 박힌다.
	reAddressShape = regexp.MustCompile(`[가-힣][가-힣0-9]*\s?\d*(로|길)\s*(지하\s*)?\d|[가-힣][가-힣0-9]*\s?\d*(동|리|가)\s*\d|\d+번지|\d+-\d+$|산\s*\d+`)
)

// normalizeAddress 지오코더를 방해하는 표기만 제거 (괄호쌍 제거·쉼표 절단·서술어 제거·번길 분리·공백 정리)
// 순서 중요: 괄호쌍을 쉼표 절단보다 먼저 지운다. '(소태동, 무등산골드클래스)' 처럼 괄호 안에
// 쉼표가 있으면, 쉼표를 먼저 자를 경우 '(소태동' 만 남아 안 닫힌 괄호로 파손 판정된다.
func normalizeAddress(addr string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}
	a = reParenPair.ReplaceAllString(a, " ") // 괄호쌍(안의 쉼표 포함) 먼저 — 안 닫힌 괄호만 파손으로 남는다
	if i := strings.Index(a, ","); i >= 0 {  // 괄호 밖 최상위 쉼표에서 절단(건물명·부가설명 제거)
		a = a[:i]
	}
	a = reDescriptive.ReplaceAllString(a, "") // '인근/부근/일대…' 서술 접미어는 통째로 버리지 말고 떼어 재시도
	a = reBungilDetach.ReplaceAllString(a, "$1 $2")
	a = reMultiSpace.ReplaceAllString(a, " ")
	return strings.TrimSpace(a)
}

// isUnfixableAddress 지오코더를 바꿔도 실패하는 주소 — normalize 를 거친 norm 을 받는 전제.
// 판정 기준은 "구체적 위치(번지/도로+번호)가 있는가" 하나로 통일한다. 서술어(reDescriptive)는
// normalizeAddress 가 이미 떼어내므로, 떼고도 도로/동/번지 형태가 남으면 시도, 남지 않으면
// (건물명·서술만 남음 = '태평인라인장', '조사리') 근사밖에 안 되므로 불가. sido 접두 유무는
// 판단 기준이 아니다 — 접두가 없는 지번주소('가좌4동 399')도 형태만 맞으면 시도한다.
// reNoSpaceLong(공백 없이 10자 이상)은 판정에서 뺐다. 카카오는 공백 없는 주소도 정상 처리한다
// (실측: '서울특별시강남구테헤란로152' → 찾음, 표본 25건 중 21건 성공). 창원시 등 일부 지자체가
// 주소를 통째로 붙여 보내는데, 이 규칙이 그걸 전부 막아 411건이 호출조차 되지 않았다.
// 쓰레기 입력('abcdefghijklmnop')은 API 가 못 찾고 not_found 로 캐시되므로 사전 차단이 불필요하다.
func isUnfixableAddress(norm string) bool {
	return !reAddressShape.MatchString(norm) ||
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
	if v := sidoStrippedVariant(norm); v != "" {
		out = append(out, v)
	}
	return out
}

// knownSido 는 정상 시도명 집합. 접두 2글자 정규식(reSidoPrefix)으로는 '경상님도' 같은
// 오타를 정상으로 오판하므로 전체 이름으로 대조한다. 통합 지자체명 변경 시 여기에 추가한다.
var knownSido = map[string]bool{
	"서울특별시": true, "부산광역시": true, "대구광역시": true, "인천광역시": true,
	"광주광역시": true, "대전광역시": true, "울산광역시": true, "세종특별자치시": true,
	"경기도": true, "강원도": true, "강원특별자치도": true, "충청북도": true, "충청남도": true,
	"전라북도": true, "전북특별자치도": true, "전라남도": true, "전남광주통합특별시": true,
	"경상북도": true, "경상남도": true, "제주도": true, "제주특별자치도": true,
	// 축약 표기도 원본 데이터에 섞여 들어온다(실측: '울산 울주군').
	"서울": true, "부산": true, "대구": true, "인천": true, "광주": true, "대전": true,
	"울산": true, "세종": true, "경기": true, "강원": true, "충북": true, "충남": true,
	"전북": true, "전남": true, "경북": true, "경남": true, "제주": true,
}

// sidoStrippedVariant 는 시도 접두를 뗀 후보를 만든다.
// 시도명이 오타('경상님도 양산시', '경상붓도 봉화군')면 접두를 고치려 애쓸 필요 없이 떼면 된다 —
// API 가 시군 이름으로 시도를 정확히 판별한다(실측):
//
//	'경상님도 양산시 황산로 719'  → 실패
//	'양산시 황산로 719'          → 경남 양산시 물금읍 황산로 719
//	'봉화군 소천면 고선리 산5-1'   → 경북 봉화군 …  (경상'북'도로 올바르게 판별)
//
// 오타 사전을 두면 '경상님도→경상남도' 로 고쳐도 봉화군은 경북이라 틀린다. 제거가 옳다.
// 정상 시도명은 건드리지 않는다(reSidoPrefix 에 매칭되면 이미 올바른 접두이므로 변형 불필요).
func sidoStrippedVariant(norm string) string {
	i := strings.Index(norm, " ")
	if i <= 0 {
		return ""
	}
	// reSidoPrefix 는 접두 2글자만 보므로 '경상님도' 도 매칭된다 — 오타 판별에 쓸 수 없다.
	// 완전한 시도명 집합으로 대조해야 한다.
	if knownSido[norm[:i]] {
		return "" // 정상 접두 — 변형 없음
	}
	// 접두가 시도 형태('...도' / '...시')인데 알려진 시도명이 아니면 오타로 보고 뗀다.
	head := norm[:i]
	if !strings.HasSuffix(head, "도") && !strings.HasSuffix(head, "시") {
		return ""
	}
	rest := strings.TrimSpace(norm[i+1:])
	// 뒤에 시/군/구가 남아야 판별 가능하다 — 그것마저 없으면 의미 없는 후보다.
	if !reSigunguHead.MatchString(rest) {
		return ""
	}
	return rest
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
