package config

import "os"

// ExpandEnvInConfig 는 config map 안의 모든 문자열 값에서 ${VAR} 를 실행 파드의
// 환경변수로 치환한다(중첩 map/slice 재귀). built-in sink 가 DSN·URL 에 하던
// os.ExpandEnv 를 stage config 전체로 일반화한 것 — 커스텀 stage 경로에는 이 해소가
// 없어서 config 의 ${VAR} 가 stage 에 원문 그대로 전달되던 문제를 메운다.
//
// env 가 없으면(값에 ${} 가 없거나 해당 변수 미설정) os.ExpandEnv 는 원문/빈문자를
// 반환하므로 평문 config 는 그대로 통과한다(기존 동작 보존).
//
// 원본을 변경하지 않고 새 map/slice 를 반환한다 — Config 는 여러 곳이 공유할 수 있다.
func ExpandEnvInConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = expandEnvValue(v)
	}
	return out
}

func expandEnvValue(v any) any {
	switch t := v.(type) {
	case string:
		return os.ExpandEnv(t)
	case map[string]any:
		return ExpandEnvInConfig(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = expandEnvValue(e)
		}
		return out
	default:
		return v
	}
}
