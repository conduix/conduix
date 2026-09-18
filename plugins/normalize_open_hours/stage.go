// normalize_open_hours — 개방시간 텍스트를 판정 가능한 컬럼으로 정규화하는 커스텀 stage
// (Plugin V3 / compile-in). 이 파일이 정본이며 plugins.SourceCode 로 등록된다.
//
// 왜 필요한가:
//   공공데이터의 개방시간은 자유 텍스트다. 실측 53,578건에 표기가 1,867 가지였다 —
//   "09:00~18:00" "0900-1800" "09시~18시" "(평일)09:00~18:00" "24시간" "연중무휴"
//   "00~24" "상시" … 그래서 "지금 열렸나" 를 물을 때마다 사람이 정규식을 새로 짜야 했다.
//   수집 시점에 한 번 정규화해 두면 화면·API 가 조건 한 줄로 판정할 수 있다.
//
// 시간대 (중요):
//   open_from/open_to 는 **KST 벽시계**다. "한국 기준 09:00 개방" 은 현지 시각 자체가
//   의미이지 순간(instant)이 아니므로 UTC 로 환산하지 않는다. 판정하는 쪽(DB 가 UTC 로
//   도는 환경)에서 CONVERT_TZ(NOW(),'+00:00','+09:00') 로 맞춰 비교한다.
//   시스템 타임스탬프(synced_at 등)는 성격이 달라 UTC 그대로 둔다.
//
// 추측하지 않는다:
//   "영업시간" "근무시간" "11시간" 처럼 시각을 알 수 없는 표기는 unknown 으로 둔다.
//   09:00~18:00 같은 기본값을 넣으면 틀린 정보를 사실처럼 보여주게 된다.
//
// 플러그인 계약:
//   - struct 이름은 반드시 Stage (registry_custom.go 가 &Stage{} 로 생성)
//   - Process 는 병렬 호출됨 → 내부 상태를 두지 않는다(이 stage 는 순수 변환)
package normalize_open_hours

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	sdk "github.com/conduix/conduix/plugin-sdk"
)

// open_status 어휘
const (
	status24h       = "24h"
	statusRanged    = "ranged"
	statusClosed    = "closed"
	statusIrregular = "irregular"
	statusUnknown   = "unknown"
)

var (
	// "24시간" "연중무휴" "상시" "00~24" "00:00~24:00" "00:00~00:00" 단독 "24"
	re24h = regexp.MustCompile(`24시간|연중무휴|^상시$|^24$|^00[~∼\-]24$|00:00[~∼\-]24:00|00:00[~∼\-]00:00`)

	// N시간(총 길이)은 시각이 아니다 — 시간 범위로 오인식되지 않게 먼저 지운다.
	reDuration = regexp.MustCompile(`[0-9]{1,2}\s*시간`)

	// 요일/수식어. 괄호는 벗기기만 한다 — (09:00~18:00) 처럼 안에 시간이 든 경우가 많아
	// 통째로 지우면 정작 시간을 잃는다(실측에서 342건이 이렇게 유실됐다).
	reNoise = regexp.MustCompile(`[()]|평일|매일|연중|무휴|개방|운영|근무|영업|시간|내|정시|상시`)

	reWeekday = regexp.MustCompile(`평일|월\s*[~∼\-]\s*금`)

	// 요일 표기는 시간 범위 앞에 붙는다("월~금 09시~18시"). 그대로 두면 구분자 `~` 가
	// 시간 범위의 것과 섞여 파싱이 깨지므로, 판정(reWeekday)을 마친 뒤 통째로 걷어낸다.
	reWeekdayPrefix = regexp.MustCompile(`(월|화|수|목|금|토|일)\s*[~∼\-]\s*(월|화|수|목|금|토|일)\s*요?일?`)

	// 파싱 가능한 세 형태
	reHHMM    = regexp.MustCompile(`^([0-9]{1,2}):([0-9]{2})[~∼\-]([0-9]{1,2}):([0-9]{2})`)
	reCompact = regexp.MustCompile(`^([0-9]{2})([0-9]{2})[~∼\-]([0-9]{2})([0-9]{2})$`)
	reHour    = regexp.MustCompile(`^([0-9]{1,2})시?[~∼\-]([0-9]{1,2})시?$`)
)

// Stage — 이름 고정 계약
type Stage struct {
	sdk.BaseNativeStage

	sourceField string // 상세 개방시간 컬럼 (기본 opn_hr_dtl)
	kindField   string // 개방구분 컬럼 (기본 opn_hr)
	prefix      string // 출력 컬럼 접두(기본 없음 → is_24h, open_from, …)
}

func (s *Stage) Init(config map[string]any) error {
	s.sourceField = strField(config, "source_field", "opn_hr_dtl")
	s.kindField = strField(config, "kind_field", "opn_hr")
	s.prefix = strField(config, "output_prefix", "")
	return nil
}

func (s *Stage) Process(record map[string]any) (map[string]any, error) {
	if record == nil {
		return nil, nil
	}
	detail := strings.TrimSpace(asString(record[s.sourceField]))
	kind := strings.TrimSpace(asString(record[s.kindField]))

	r := ParseOpenHours(kind, detail)

	record[s.prefix+"open_status"] = r.Status
	record[s.prefix+"is_24h"] = boolToInt(r.Is24h)
	record[s.prefix+"weekday_only"] = boolToInt(r.WeekdayOnly)
	record[s.prefix+"overnight"] = boolToInt(r.Overnight)
	record[s.prefix+"open_parse_src"] = truncate(detail, 255)

	// 시각이 없으면 NULL 로 둔다 — 0 을 넣으면 "00:00 개방" 과 구분되지 않는다.
	if r.Status == statusRanged {
		record[s.prefix+"open_from"] = r.From
		record[s.prefix+"open_to"] = r.To
	} else {
		record[s.prefix+"open_from"] = nil
		record[s.prefix+"open_to"] = nil
	}
	return record, nil
}

// Result 는 정규화 결과다.
type Result struct {
	Status      string
	Is24h       bool
	From, To    string // "HH:MM:SS" (KST 벽시계)
	Overnight   bool
	WeekdayOnly bool
	Src         string // 파싱 근거 원문(검증·디버깅용)
}

// ParseOpenHours 는 개방구분·상세텍스트로부터 판정 가능한 형태를 만든다.
// 순수 함수라 테스트로 규칙 전체를 고정할 수 있다.
func ParseOpenHours(kind, detail string) Result {
	res := Result{Status: statusUnknown, Src: truncate(detail, 255)}
	res.WeekdayOnly = reWeekday.MatchString(detail)

	// 상시인데 상세가 비어 있으면 24시간이다.
	// 실측: '상시' 22,308건 중 21,378건(95.8%)이 상세가 비어 있었다 —
	// 시간 범위만 찾으면 이 2만여 건이 통째로 unknown 이 된다.
	if kind == "상시" && detail == "" {
		res.Status, res.Is24h = status24h, true
		return res
	}
	if detail != "" && re24h.MatchString(detail) {
		res.Status, res.Is24h = status24h, true
		return res
	}
	if kind == "미개방" || strings.Contains(detail, "미개방") {
		res.Status = statusClosed
		return res
	}
	if kind == "불규칙" {
		res.Status = statusIrregular
		return res
	}

	// 순서가 중요하다: 요일 범위(월~금) → 총 길이(N시간) → 나머지 수식어.
	// 요일을 먼저 걷어내지 않으면 그 `~` 가 시간 구분자로 오인식된다.
	cleaned := reWeekdayPrefix.ReplaceAllString(detail, "")
	cleaned = reNoise.ReplaceAllString(reDuration.ReplaceAllString(cleaned, ""), "")
	cleaned = strings.Join(strings.Fields(cleaned), "")

	from, to, ok := parseRange(cleaned)
	if !ok {
		return res
	}
	res.Status = statusRanged
	res.From, res.To = from, to
	res.Overnight = to <= from // 22:00~02:00 처럼 자정을 넘기는 운영
	return res
}

func parseRange(s string) (from, to string, ok bool) {
	if m := reHHMM.FindStringSubmatch(s); m != nil {
		return clock(m[1], m[2]), clock(m[3], m[4]), true
	}
	if m := reCompact.FindStringSubmatch(s); m != nil { // 0900-1800
		return clock(m[1], m[2]), clock(m[3], m[4]), true
	}
	if m := reHour.FindStringSubmatch(s); m != nil { // 09~18, 09시~18시
		return clock(m[1], "00"), clock(m[2], "00"), true
	}
	return "", "", false
}

// clock 은 HH:MM:SS 로 맞춘다. 24:00 은 종료 표기로 흔해 23:59:59 로 접는다
// (MySQL TIME 은 24:00:00 을 받지만, BETWEEN 비교에서 다음날 00:00 과 헷갈린다).
func clock(h, m string) string {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	if hh >= 24 {
		return "23:59:59"
	}
	if hh > 23 || mm > 59 {
		return "23:59:59"
	}
	return fmt.Sprintf("%02d:%02d:00", hh, mm)
}

func strField(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key]; ok {
		if s := asString(v); s != "" {
			return s
		}
	}
	return def
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
