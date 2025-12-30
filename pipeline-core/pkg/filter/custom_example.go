// Package filter 커스텀 필터 예시
//
// 새로운 필터 연산자를 추가하는 방법:
//
//  1. 이 파일에 연산자 등록 추가
//  2. evaluator.go에 평가 로직 추가
//  3. `go generate`로 프론트엔드 타입 동기화
//
// 필터를 삭제하는 방법:
//
//  1. 이 파일에서 Register 호출 제거
//  2. evaluator.go에서 해당 로직 제거
//  3. `go generate`로 프론트엔드 타입 동기화
package filter

func init() {
	// ========================================
	// 커스텀 필터 연산자 등록 예시
	// 아래 주석을 해제하여 사용하세요
	// ========================================

	// 예시 1: IP 주소 범위 체크
	// RegisterCustom(
	// 	"ip_in_range",           // ID (코드에서 사용)
	// 	"IP 범위 내",             // 라벨 (GUI 표시)
	// 	"IP가 CIDR 범위에 포함되는지 확인", // 설명
	// 	true,                    // 값이 필요한지
	// 	"string",                // 값 타입 (string, number, array, regex)
	// 	"network",               // 카테고리
	// )

	// 예시 2: 날짜/시간 비교
	// RegisterCustom(
	// 	"date_after",
	// 	"날짜 이후",
	// 	"지정한 날짜 이후인지 확인",
	// 	true,
	// 	"string", // ISO8601 형식
	// 	"datetime",
	// )

	// RegisterCustom(
	// 	"date_before",
	// 	"날짜 이전",
	// 	"지정한 날짜 이전인지 확인",
	// 	true,
	// 	"string",
	// 	"datetime",
	// )

	// 예시 3: JSON Path 존재 확인
	// RegisterCustom(
	// 	"jsonpath_exists",
	// 	"JSON Path 존재",
	// 	"JSON Path가 존재하는지 확인",
	// 	true,
	// 	"string",
	// 	"json",
	// )

	// ========================================
	// 필터 삭제: 위의 RegisterCustom 호출을 제거하면
	// go generate 실행 시 프론트엔드에서도 자동으로 제거됩니다
	// ========================================
}

// ========================================
// 커스텀 필터 평가 로직 추가 가이드
// ========================================
//
// evaluator.go의 compare 메서드에 case 추가:
//
//   case "ip_in_range":
//       return evalIPInRange(fieldValue, compareValue)
//
// 평가 함수 구현:
//
//   func evalIPInRange(fieldValue any, cidr any) (bool, error) {
//       ip := net.ParseIP(toString(fieldValue))
//       _, network, err := net.ParseCIDR(toString(cidr))
//       if err != nil {
//           return false, err
//       }
//       return network.Contains(ip), nil
//   }
