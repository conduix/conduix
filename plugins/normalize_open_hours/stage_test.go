package normalize_open_hours

import "testing"

// 실측 데이터(restrooms 53,578건, 표기 1,867가지)에서 뽑은 대표 표기로 규칙을 고정한다.
// 건수는 2026-09-17 기준 실측치 — 규칙을 고치다 큰 덩어리를 놓치면 여기서 걸린다.
func TestParseOpenHours(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		detail   string
		want     Result
	}{
		// --- 24시간: 시간 범위가 아예 없는 것이 다수다 ---
		{"상시+상세없음(21,378건)", "상시", "", Result{Status: status24h, Is24h: true}},
		{"24시간", "", "24시간", Result{Status: status24h, Is24h: true}},
		{"24시간 개방", "", "24시간 개방", Result{Status: status24h, Is24h: true}},
		{"연중무휴", "", "연중무휴", Result{Status: status24h, Is24h: true}},
		{"상시(상세)", "상시", "상시", Result{Status: status24h, Is24h: true}},
		{"00:00~24:00", "", "00:00~24:00", Result{Status: status24h, Is24h: true}},
		{"00~24", "", "00~24", Result{Status: status24h, Is24h: true}},
		{"단독 24", "", "24", Result{Status: status24h, Is24h: true}},

		// --- 시간 범위 ---
		{"HH:MM~HH:MM(8,885건)", "정시", "09:00~18:00",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"하이픈", "정시", "09:00-18:00",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"전각물결", "정시", "09:00∼18:00",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"공백 포함", "정시", "09:00 ~ 18:00",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"HHMM-HHMM(135건)", "", "0900-1800",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"HH~HH(238건)", "", "09~18",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"HH시~HH시(117건)", "", "09시~18시",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},

		// 괄호는 벗긴다 — 통째로 지우면 안의 시간을 잃는다(실측 342건이 이 경우)
		{"(평일)접두(655건)", "정시", "(평일)09:00~18:00",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00", WeekdayOnly: true}},
		{"괄호로 감쌈(118건)", "", "(09:00~18:00)",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"정시(...)(224건)", "정시", "정시(09:00~18:00)",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00"}},
		{"월~금(204건)", "", "월~금 09시~18시",
			Result{Status: statusRanged, From: "09:00:00", To: "18:00:00", WeekdayOnly: true}},

		// 자정 넘김
		{"자정 넘김", "", "22:00~02:00",
			Result{Status: statusRanged, From: "22:00:00", To: "02:00:00", Overnight: true}},
		{"24:00 종료는 23:59:59 로 접음", "", "05:00~24:00",
			Result{Status: statusRanged, From: "05:00:00", To: "23:59:59"}},

		// --- 판정 불가·닫힘 ---
		{"미개방", "미개방", "", Result{Status: statusClosed}},
		{"불규칙", "불규칙", "협의", Result{Status: statusIrregular}},
		{"N시간은 길이일 뿐 시각이 아니다(291건)", "정시", "11시간", Result{Status: statusUnknown}},
		{"영업시간", "정시", "영업시간", Result{Status: statusUnknown}},
		{"근무시간", "정시", "근무시간", Result{Status: statusUnknown}},
		{"빈 껍데기(100건)", "상시", ":~:", Result{Status: statusUnknown}},
		{"하이픈만(114건)", "", "-", Result{Status: statusUnknown}},
		{"완전 공백", "", "", Result{Status: statusUnknown}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseOpenHours(c.kind, c.detail)
			if got.Status != c.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, c.want.Status)
			}
			if got.Is24h != c.want.Is24h {
				t.Errorf("Is24h = %v, want %v", got.Is24h, c.want.Is24h)
			}
			if got.From != c.want.From || got.To != c.want.To {
				t.Errorf("범위 = %q~%q, want %q~%q", got.From, got.To, c.want.From, c.want.To)
			}
			if got.Overnight != c.want.Overnight {
				t.Errorf("Overnight = %v, want %v", got.Overnight, c.want.Overnight)
			}
			if got.WeekdayOnly != c.want.WeekdayOnly {
				t.Errorf("WeekdayOnly = %v, want %v", got.WeekdayOnly, c.want.WeekdayOnly)
			}
		})
	}
}

// 판정 불가를 그럴듯한 기본값으로 채우면 틀린 정보를 사실처럼 보여주게 된다.
func TestUnknownNeverGetsFabricatedHours(t *testing.T) {
	for _, d := range []string{"영업시간", "11시간", "-", ":~:", "협의후 이용"} {
		got := ParseOpenHours("정시", d)
		if got.From != "" || got.To != "" {
			t.Errorf("%q 에 시각이 지어졌다: %q~%q", d, got.From, got.To)
		}
	}
}

// Process 는 레코드에 컬럼을 채운다. ranged 가 아니면 시각은 NULL 이어야 한다
// (0 을 넣으면 "00:00 개방" 과 구분되지 않는다).
func TestProcess_SetsColumns(t *testing.T) {
	s := &Stage{}
	if err := s.Init(map[string]any{}); err != nil {
		t.Fatalf("init: %v", err)
	}

	rec, err := s.Process(map[string]any{"opn_hr": "정시", "opn_hr_dtl": "09:00~18:00"})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if rec["open_status"] != statusRanged {
		t.Errorf("open_status = %v", rec["open_status"])
	}
	if rec["open_from"] != "09:00:00" || rec["open_to"] != "18:00:00" {
		t.Errorf("범위 = %v~%v", rec["open_from"], rec["open_to"])
	}
	if rec["is_24h"] != 0 {
		t.Errorf("is_24h = %v, want 0", rec["is_24h"])
	}

	rec24, _ := s.Process(map[string]any{"opn_hr": "상시", "opn_hr_dtl": ""})
	if rec24["is_24h"] != 1 {
		t.Errorf("24시간인데 is_24h = %v", rec24["is_24h"])
	}
	if rec24["open_from"] != nil || rec24["open_to"] != nil {
		t.Errorf("24시간은 시각이 NULL 이어야 한다: %v~%v", rec24["open_from"], rec24["open_to"])
	}
}
