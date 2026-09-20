package source

import (
	"testing"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// API 키는 쿼리 파라미터나 헤더로 간다. 둘 다 ${VAR} 로 쓸 수 있어야
// 워크플로 설정(DB 에 저장되고 화면·API 로 노출된다)에 평문 키를 두지 않는다.
func TestHTTPSource_ExpandsEnvInURLAndHeaders(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-value-123")
	t.Setenv("TEST_HEADER_KEY", "header-secret")

	src, err := NewHTTPSource(config.SourceV2{
		Type:   "rest_api",
		URL:    "https://api.example.com/data?serviceKey=${TEST_API_KEY}&type=json",
		Method: "GET",
		Headers: map[string]string{
			"Authorization": "KakaoAK ${TEST_HEADER_KEY}",
			"Accept":        "application/json",
		},
	})
	if err != nil {
		t.Fatalf("NewHTTPSource: %v", err)
	}

	want := "https://api.example.com/data?serviceKey=secret-value-123&type=json"
	if src.url != want {
		t.Errorf("url = %q, want %q", src.url, want)
	}
	if got := src.headers["Authorization"]; got != "KakaoAK header-secret" {
		t.Errorf("Authorization = %q", got)
	}
	// 치환할 것이 없는 값은 그대로 둔다.
	if got := src.headers["Accept"]; got != "application/json" {
		t.Errorf("Accept = %q — 건드리지 말아야 한다", got)
	}
}

// 헤더가 없으면 nil 로 남긴다 — 빈 맵을 만들면
// "헤더 없음" 과 "빈 헤더 설정" 이 구분되지 않는다.
func TestExpandEnvVarsInMap_NilStaysNil(t *testing.T) {
	if got := expandEnvVarsInMap(nil); got != nil {
		t.Errorf("nil 입력에 %v 를 돌려줬다", got)
	}
}

// 환경변수가 없으면 os.ExpandEnv 규칙대로 빈 문자열이 된다.
// 키가 주입되지 않은 채 실행되면 인증이 실패하는데, 그 편이
// 평문 키가 남는 것보다 낫다(실패가 드러난다).
func TestHTTPSource_MissingEnvBecomesEmpty(t *testing.T) {
	t.Setenv("DEFINITELY_UNSET_KEY_FOR_TEST", "")

	src, err := NewHTTPSource(config.SourceV2{
		Type: "rest_api",
		URL:  "https://api.example.com/d?k=${DEFINITELY_UNSET_KEY_FOR_TEST}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if src.url != "https://api.example.com/d?k=" {
		t.Errorf("url = %q", src.url)
	}
}
