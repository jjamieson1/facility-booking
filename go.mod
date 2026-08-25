module github.com/jjamieson1/facility-booking

go 1.25.0

// Pinned to the toolchain that patches the standard-library CVEs
// govulncheck reports against 1.25.5 (net/url, html/template, crypto/tls,
// crypto/x509, net/http, encoding/asn1). 22 of the 25 findings are stdlib, so
// the toolchain is the fix — there is no dependency to bump for them.
toolchain go1.25.13

require (
	github.com/coreos/go-oidc/v3 v3.20.0
	github.com/go-chi/chi/v5 v5.3.0
	github.com/google/uuid v1.6.0
	golang.org/x/oauth2 v0.36.0
	gorm.io/driver/mysql v1.5.7
	gorm.io/gorm v1.25.12
)

require (
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	golang.org/x/text v0.39.0 // indirect
)
