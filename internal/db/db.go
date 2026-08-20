// Package db opens the GORM connection to MariaDB/MySQL and runs AutoMigrate.
//
// MariaDB is the only supported database. There was once a pure-Go SQLite
// fallback so the app would boot with no setup; it was removed because nothing
// ran on it. Two things made it a liability rather than a convenience:
//
//   - `SELECT … FOR UPDATE` is a no-op on SQLite and foreign keys are not
//     enforced, so a green test run on SQLite proved nothing about the booking
//     transaction — it hid a deadlock and a silent double-booking.
//   - A fallback means a missing or malformed DSN yields an app that boots
//     healthy while writing bookings to a local file nobody backs up.
package db

import (
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Open connects using cfg, applies AutoMigrate for every model, and returns the
// handle. It fails fast — a misconfigured DSN must stop the app, not degrade it.
func Open(cfg config.Config) (*gorm.DB, error) {
	if strings.TrimSpace(cfg.DBDSN) == "" {
		return nil, fmt.Errorf("db: FB_DB_DSN is not set — MariaDB is required; " +
			"create the database with scripts/db-setup.sql, then set e.g. " +
			"FB_DB_DSN='facility_app:PASSWORD@tcp(127.0.0.1:3306)/facility_booking?parseTime=true&loc=UTC&charset=utf8mb4'")
	}

	gcfg := &gorm.Config{Logger: logger.Default.LogMode(logLevel(cfg))}
	gdb, err := gorm.Open(mysql.Open(cfg.DBDSN), gcfg)
	if err != nil {
		// Never include the DSN in the error: it carries the password.
		return nil, fmt.Errorf("db: connect to MariaDB: %w", err)
	}
	if err := gdb.AutoMigrate(domain.AllModels()...); err != nil {
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return gdb, nil
}

// logLevel keeps GORM quiet by default (warnings + errors only); set
// FB_DB_LOG=info via the caller if full SQL tracing is ever needed.
func logLevel(config.Config) logger.LogLevel {
	return logger.Warn
}
