module normalize_open_hours

go 1.27

require (
	github.com/conduix/conduix/plugin-sdk v0.0.0
	github.com/go-sql-driver/mysql v1.10.1
)

require filippo.io/edwards25519 v1.2.0 // indirect

replace github.com/conduix/conduix/plugin-sdk => ../../plugin-sdk
