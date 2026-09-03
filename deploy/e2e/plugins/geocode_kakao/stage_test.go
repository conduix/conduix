package geocode_kakao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// --- 주소 정규화 (문서 '주소 정규화 규칙'의 예시 그대로) ---

func TestNormalizeAddress(t *testing.T) {
	tests := []struct{ in, want string }{
		{"경기도 남양주시 도농1길 69, 화장실", "경기도 남양주시 도농1길 69"},
		{"서울 강북구 삼양로 78 (길음동)", "서울 강북구 삼양로 78"},
		{"대전 유성구 구즉로58번길15-12", "대전 유성구 구즉로58번길 15-12"},
		{"서울  강남구   테헤란로  1", "서울 강남구 테헤란로 1"},
		{"  ", ""},
		{"경기도 평택시 고덕로 283(좌교리", "경기도 평택시 고덕로 283(좌교리"}, // 안 닫힌 괄호 보존 → 파손 판정
	}
	for _, tt := range tests {
		if got := normalizeAddress(tt.in); got != tt.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsUnfixableAddress(t *testing.T) {
	unfixable := []string{
		"경기도 성남시 수정구 태평인라인장 앞",
		"경상북도 포항시 북구 송라면 조사리 해안가",
		"남문로 352",
		"경상남도마산합포구제2두부로30",
		"강원특별자치도 횡성군 우천면 우항리 583-2외 8필지",
		"경기도 평택시 고덕면 고덕로 283(좌교리",
	}
	fixable := []string{
		"강원특별자치도 양양군 현남면 인구길 33",
		"인천광역시 서해구 가좌동 399",
		"전남광주통합특별시 곡성군 삼기면 곡순로 1436",
	}
	for _, a := range unfixable {
		if !isUnfixableAddress(a) {
			t.Errorf("expected unfixable: %q", a)
		}
	}
	for _, a := range fixable {
		if isUnfixableAddress(a) {
			t.Errorf("expected fixable: %q", a)
		}
	}
}

func TestVariants(t *testing.T) {
	if got := spacingVariants("서울 강서구 수리골길17"); len(got) == 0 || got[0] != "서울 강서구 수리골길 17" {
		t.Errorf("spacing detach: %v", got)
	}
	if got := lotNotationVariant("전남 곡성군 죽곡면 원달리 산 52번지 18호"); got != "전남 곡성군 죽곡면 원달리 산52-18" {
		t.Errorf("lot notation: %q", got)
	}
	if got := sublotVariants("인천 서해구 가좌동 399"); len(got) != 5 || got[0] != "인천 서해구 가좌동 399-1" {
		t.Errorf("sublot: %v", got)
	}
	if got := sublotVariants("인천 서해구 가좌동 399-1"); got != nil {
		t.Errorf("sublot on existing sublot: %v", got)
	}
}

// --- 플러그인 인터페이스 흐름 (httptest 모의 카카오) ---

func mockKakao(t *testing.T, handler func(q string) (lat, lon string, road, found bool)) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "KakaoAK test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lat, lon, road, found := handler(r.URL.Query().Get("query"))
		docs := []any{}
		if found {
			doc := map[string]any{"x": lon, "y": lat,
				"address": map[string]any{"region_1depth_name": "인천", "region_2depth_name": "서구"}}
			if road {
				doc["road_address"] = map[string]any{}
			}
			docs = append(docs, doc)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": docs})
	}))
	return srv, calls
}

func newTestStage(t *testing.T, baseURL, apiKey string) *Stage {
	t.Helper()
	s := &Stage{}
	err := s.Init(map[string]any{
		"api_key": apiKey, "api_base_url": baseURL,
		"address_field": "road_addr", "lotno_field": "lotno_addr",
		"rps": 1000.0,
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return s
}

func TestProcess_SuccessCacheHitAndAddressChange(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5665", "126.9780", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, err := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if rec["lat"] != 37.5665 || rec["geo_match"] != "road" || rec["geo_status"] != "ok" {
		t.Errorf("attach wrong: %v", rec)
	}

	// 같은 주소 → 캐시 히트 (API 재호출 없음) — "이미 좌표 있으면 동작 안 함"
	before := calls.Load()
	rec2, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if calls.Load() != before || rec2["lat"] != 37.5665 {
		t.Errorf("expected cache hit: calls %d→%d, %v", before, calls.Load(), rec2)
	}

	// 주소 변경 → 캐시 미스 → 재지오코딩
	_, _ = s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 999"})
	if calls.Load() == before {
		t.Errorf("expected new API call for changed address")
	}
}

func TestProcess_CacheOnlyModeWithoutKey(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5", "127.0", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "") // 키 없음 — 캐시 전용 모드

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if calls.Load() != 0 {
		t.Errorf("no key must not call API")
	}
	if rec["geo_status"] != "no_api_key" {
		t.Errorf("expected no_api_key marker, got %v", rec["geo_status"])
	}
}

func TestProcess_UnfixableSkipsAPI(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5", "127.0", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "경기도 성남시 수정구 태평인라인장 앞"})
	if calls.Load() != 0 {
		t.Errorf("unfixable must not reach API")
	}
	if rec["geo_status"] != "unfixable" {
		t.Errorf("expected unfixable marker, got %v", rec["geo_status"])
	}
	if _, ok := rec["lat"]; ok {
		t.Errorf("unfixable must not have coords")
	}
}

func TestProcess_LotnoFallback(t *testing.T) {
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		if q == "인천광역시 서구 가좌동 399" {
			return "37.49", "126.67", false, true
		}
		return "", "", false, false
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{
		"road_addr":  "서울특별시 어딘가 이상한주소 1",
		"lotno_addr": "인천광역시 서구 가좌동 399",
	})
	if rec["lat"] != 37.49 || rec["geo_match"] != "region" {
		t.Errorf("lotno fallback failed: %v", rec)
	}
}

func TestProcess_OutOfBoundsRejected(t *testing.T) {
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "35.6762", "139.6503", true, true // 도쿄 — 오매칭
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if _, ok := rec["lat"]; ok {
		t.Errorf("out-of-bounds must be rejected: %v", rec)
	}
	if rec["geo_status"] != "not_found" {
		t.Errorf("expected not_found, got %v", rec["geo_status"])
	}
}

func TestProcess_Retry429(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{
			map[string]any{"x": "126.978", "y": "37.5665", "road_address": map[string]any{}},
		}})
	}))
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if rec["lat"] != 37.5665 || attempt != 2 {
		t.Errorf("expected success after 1 retry (attempts=%d): %v", attempt, rec)
	}
}

func TestProcess_NoAddressMarker(t *testing.T) {
	s := newTestStage(t, "http://unused", "test-key")
	rec, err := s.Process(map[string]any{"name": "화장실"})
	if err != nil || rec["geo_status"] != "no_address" {
		t.Errorf("expected no_address marker: %v %v", rec, err)
	}
}

func TestInit_Validation(t *testing.T) {
	if err := (&Stage{}).Init(map[string]any{}); err == nil {
		t.Error("expected error without address_field")
	}
}
