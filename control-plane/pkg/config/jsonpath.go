package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ExtractJSONPath JSON 데이터에서 경로를 따라 값을 추출
// 경로 형식: "field", "parent.child", "response.user.email" 등
func ExtractJSONPath(data map[string]interface{}, path string) (interface{}, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}

	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return nil, fmt.Errorf("key '%s' not found in path '%s'", part, path)
			}
			current = val
		case []interface{}:
			// 배열 인덱스 지원 (예: "items.0.name")
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("expected array index but got '%s'", part)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("array index %d out of bounds", idx)
			}
			current = v[idx]
		default:
			return nil, fmt.Errorf("cannot navigate further from type %T at '%s'", current, part)
		}
	}

	return current, nil
}

// ExtractJSONPathString JSON 데이터에서 문자열 값 추출
func ExtractJSONPathString(data map[string]interface{}, path string) string {
	if path == "" {
		return ""
	}

	val, err := ExtractJSONPath(data, path)
	if err != nil {
		return ""
	}

	switch v := val.(type) {
	case string:
		return v
	case float64:
		// JSON 숫자는 float64로 파싱됨
		return fmt.Sprintf("%.0f", v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ParseUserInfoFromJSON OAuth 응답에서 사용자 정보 추출
type ParsedUserInfo struct {
	ID        string
	Email     string
	Name      string
	AvatarURL string
}

// ParseUserInfo JSON 응답과 매핑 설정을 사용해 사용자 정보 추출
func ParseUserInfo(jsonData []byte, mapping UserMappingConfig) (*ParsedUserInfo, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &ParsedUserInfo{
		ID:        ExtractJSONPathString(data, mapping.ID),
		Email:     ExtractJSONPathString(data, mapping.Email),
		Name:      ExtractJSONPathString(data, mapping.Name),
		AvatarURL: ExtractJSONPathString(data, mapping.Avatar),
	}, nil
}
