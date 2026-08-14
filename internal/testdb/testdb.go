// Package testdb hands tests a migrated MariaDB database.
//
// MariaDB is the only target, because it is the only database this app runs on.
// There was an in-memory SQLite mode so `go test ./...` needed no setup; it was
// removed after it produced a green suite for code that had a deadlock and a
// silent double-booking. SQLite treats `SELECT … FOR UPDATE` as a no-op and does
// not enforce foreign keys, so a pass there said nothing about the behaviour
// that matters most here.
//
// Set FB_TEST_MYSQL_DSN, with **no database name** — each call to New creates its
// own throwaway database and drops it on cleanup, so packages running in
// parallel cannot see each other's rows:
//
//	FB_TEST_MYSQL_DSN='facility_app:pw@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC&charset=utf8mb4' go test ./... -p 4
//
// Use -p 4: the default is one package per core, and ~15 packages each running
// CREATE DATABASE plus a full AutoMigrate at once turns a 75-second suite into
// about nine minutes of server contention.
package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// DSNEnv names the environment variable carrying the base MariaDB DSN.
const DSNEnv = "FB_TEST_MYSQL_DSN"

// New returns a migrated, isolated database for one test. It fails the test
// immediately if MariaDB is not configured — skipping instead would let a whole
// suite "pass" having verified nothing.
func New(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(DSNEnv))
	if dsn == "" {
		t.Fatalf("%s is not set. These tests run against MariaDB only.\n"+
			"  Create the database and grants:  mariadb -u root -p < scripts/db-setup.sql\n"+
			"  Then export a DSN with NO database name, e.g.\n"+
			"  %s='facility_app:PASSWORD@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC&charset=utf8mb4'\n"+
			"  (or put it in .env and run: set -a; . ./.env; set +a; go test ./... -p 4)",
			DSNEnv, DSNEnv)
	}

	name := "fbtest_" + randomSuffix(t)
	admin, err := gorm.Open(mysql.Open(dsn), config())
	if err != nil {
		t.Fatalf("testdb: connect to MariaDB (%s): %v", DSNEnv, err)
	}
	if err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		t.Fatalf("testdb: create %s: %v", name, err)
	}

	db, err := gorm.Open(mysql.Open(withDatabase(dsn, name)), config())
	if err != nil {
		t.Fatalf("testdb: open %s: %v", name, err)
	}
	if err := db.AutoMigrate(domain.AllModels()...); err != nil {
		t.Fatalf("testdb: migrate %s: %v", name, err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		if err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
			t.Logf("testdb: drop %s: %v", name, err)
		}
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// withDatabase inserts the database name into a DSN of the form
// user:pw@tcp(host:port)/[db][?params].
func withDatabase(dsn, name string) string {
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 {
		return dsn + "/" + name
	}
	head, tail := dsn[:slash+1], dsn[slash+1:]
	if q := strings.Index(tail, "?"); q >= 0 {
		return head + name + tail[q:]
	}
	return head + name
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("testdb: random: %v", err)
	}
	return hex.EncodeToString(b)
}

func config() *gorm.Config {
	// Errors only: the suite deliberately exercises not-found paths, and GORM's
	// default warn level makes that noise drown the actual failures.
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Error)}
}
