// 기존 수집분 일괄 정규화.
//
// stage 는 신규·변경 레코드만 처리하므로 이미 쌓인 데이터는 한 번 채워야 한다.
// 규칙을 SQL 로 다시 쓰지 않고 stage 와 **같은 parseOpenHours 를 호출**한다 —
// 두 벌로 두면 한쪽만 고쳤을 때 신규분과 기존분의 판정이 갈린다.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"

	noh "normalize_open_hours"
)

func main() {
	dsn := flag.String("dsn", "", "MySQL DSN (user:pass@tcp(host:port)/db)")
	table := flag.String("table", "restrooms", "대상 테이블")
	dry := flag.Bool("dry-run", false, "쓰지 않고 집계만")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("--dsn 필요")
	}
	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf(
		"SELECT mng_no, COALESCE(opn_hr,''), COALESCE(opn_hr_dtl,'') FROM %s", *table))
	if err != nil {
		log.Fatal(err)
	}

	type upd struct {
		id string
		r  noh.Result
	}
	var batch []upd
	counts := map[string]int{}
	for rows.Next() {
		var id, kind, detail string
		if err := rows.Scan(&id, &kind, &detail); err != nil {
			log.Fatal(err)
		}
		r := noh.ParseOpenHours(kind, detail)
		counts[r.Status]++
		batch = append(batch, upd{id, r})
	}
	rows.Close()

	total := 0
	for _, n := range counts {
		total += n
	}
	fmt.Printf("총 %d건\n", total)
	for _, k := range []string{"24h", "ranged", "unknown", "closed", "irregular"} {
		fmt.Printf("  %-10s %6d (%.1f%%)\n", k, counts[k], float64(counts[k])*100/float64(total))
	}
	if *dry {
		fmt.Println("(dry-run — 쓰지 않음)")
		return
	}

	stmt, err := db.Prepare(fmt.Sprintf(`UPDATE %s SET
	  open_status=?, is_24h=?, open_from=?, open_to=?,
	  overnight=?, weekday_only=?, open_parse_src=? WHERE mng_no=?`, *table))
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	n := 0
	for _, u := range batch {
		var from, to any
		if u.r.Status == "ranged" {
			from, to = u.r.From, u.r.To
		}
		if _, err := stmt.Exec(u.r.Status, b2i(u.r.Is24h), from, to,
			b2i(u.r.Overnight), b2i(u.r.WeekdayOnly), u.r.Src, u.id); err != nil {
			log.Fatalf("update %s: %v", u.id, err)
		}
		n++
		if n%5000 == 0 {
			fmt.Printf("  ... %d건 적용\n", n)
		}
	}
	fmt.Printf("%d건 적용 완료\n", n)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
