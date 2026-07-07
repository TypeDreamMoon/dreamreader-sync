module github.com/TypeDreamMoon/dreamreader-sync

go 1.26

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/hertz-iam/authmw-go v0.0.0
	modernc.org/sqlite v1.53.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.44.0 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// dreamreader-sync is a standalone, app-owned service. Its only cross-repo
// coupling is the canonical IAM token validator, resolved from the sibling
// hertz-iam checkout so it always tracks the platform's JWKS/JWT rules.
replace github.com/hertz-iam/authmw-go => ../hertz-iam/packages/authmw-go
