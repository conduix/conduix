// CSV 배포본으로 restrooms 시설 상세를 보강한다.
//
// API(15155058) 수집분에는 없는 장애인용·어린이용 변기수, 기저귀교환대/비상벨
// 위치 등을 CSV(15012892)에서 가져와 mng_no 기준으로 채운다.
// 두 배포본은 관리번호가 99.99% 겹치므로 UPDATE 로 붙는다(신규 INSERT 안 함 —
// 수집 주체는 API 파이프라인이고 이 도구는 보강만 한다).
//
// 읽기는 pipeline-core 의 http source 를 그대로 쓴다. 인코딩·CSV 파싱 규칙을
// 따로 구현하면 파이프라인과 두 벌이 되어 어긋난다.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/source"
)

const csvURL = "https://file.localdata.go.kr/file/download/public_restroom_info/info"

func main() {
	dsn := flag.String("dsn", "", "MySQL DSN")
	dry := flag.Bool("dry-run", false, "쓰지 않고 집계만")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("--dsn 필요")
	}

	// Referer 가 없으면 403 이다(실측). 서버가 charset=UTF-8 이라 응답하지만
	// 실제 본문은 cp949 이므로 명시한다.
	src, err := source.NewHTTPSource(config.SourceV2{
		Type: "rest_api", URL: csvURL, Method: "GET",
		Format: "csv", Encoding: "cp949",
		Headers: map[string]string{
			"Referer":    "https://www.localdata.go.kr/",
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/151.0.0.0 Safari/537.36",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	stmt, err := db.Prepare(`UPDATE restrooms SET
	  male_toilet_cnt=?, male_urinal_cnt=?, male_disabled_toilet_cnt=?, male_disabled_urinal_cnt=?,
	  male_child_toilet_cnt=?, male_child_urinal_cnt=?,
	  female_toilet_cnt=?, female_disabled_toilet_cnt=?, female_child_toilet_cnt=?,
	  diaper_place=?, bell_place=?, owner_type=?, waste_type=?, safety_target_yn=?,
	  law_basis=?, install_ym=?, remodel_ym=?, csv_synced_at=?
	 WHERE mng_no=?`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	ctx := context.Background()
	recs, errs := src.Read(ctx)

	now := time.Now()
	var total, updated, notFound int
	for r := range recs {
		total++
		d := r.Data
		id := str(d["관리번호"])
		if id == "" {
			continue
		}
		if *dry {
			continue
		}
		res, err := stmt.Exec(
			num(d["남성용-대변기수"]), num(d["남성용-소변기수"]),
			num(d["남성용-장애인용대변기수"]), num(d["남성용-장애인용소변기수"]),
			num(d["남성용-어린이용대변기수"]), num(d["남성용-어린이용소변기수"]),
			num(d["여성용-대변기수"]), num(d["여성용-장애인용대변기수"]), num(d["여성용-어린이용대변기수"]),
			nz(d["기저귀교환대장소"]), nz(d["비상벨설치장소"]), nz(d["화장실소유구분명"]),
			nz(d["오물처리방식"]), nz(d["안전관리시설설치대상여부"]), nz(d["근거법령명"]),
			nz(d["설치연월"]), nz(d["리모델링연월"]), now, id)
		if err != nil {
			log.Fatalf("update %s: %v", id, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
		} else {
			notFound++ // CSV 에는 있는데 DB 에 없는 신규 — 수집은 API 파이프라인 몫
		}
		if updated%5000 == 0 && updated > 0 {
			fmt.Printf("  ... %d건 보강\n", updated)
		}
	}
	if e := <-errs; e != nil {
		log.Fatalf("read: %v", e)
	}

	fmt.Printf("CSV %d건 / 보강 %d건 / DB 에 없음 %d건\n", total, updated, notFound)
	if *dry {
		fmt.Println("(dry-run — 쓰지 않음)")
	}
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// nz 는 빈 문자열을 NULL 로 만든다 — ""(모름)과 실제 값을 구분해야 한다.
func nz(v any) any {
	if s := str(v); s != "" {
		return s
	}
	return nil
}

// num 은 숫자로 못 읽으면 NULL 이다. 0 을 넣으면 "변기 0개" 와 "정보 없음" 이 섞인다.
func num(v any) any {
	s := str(v)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return n
}
