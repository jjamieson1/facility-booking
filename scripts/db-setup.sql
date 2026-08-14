-- MariaDB / MySQL setup for facility-booking.
--
-- The application creates and migrates all tables itself (GORM AutoMigrate on
-- boot), so this script only creates the database and a least-privilege app
-- user. Run it once as an admin, AFTER setting a real password below.
--
--   mariadb -u root -p < scripts/db-setup.sql
--
-- Then point the API at it:
--   FB_DB_DRIVER=mysql \
--   FB_DB_DSN='facility_app:CHANGE_ME@tcp(127.0.0.1:3306)/facility_booking?parseTime=true&loc=UTC&charset=utf8mb4' \
--   go run ./cmd/server

CREATE DATABASE IF NOT EXISTS facility_booking
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

-- Replace CHANGE_ME with a strong password and use the SAME value in FB_DB_DSN.
-- Use '127.0.0.1' rather than 'localhost' to force TCP (matches the tcp() DSN).
CREATE USER IF NOT EXISTS 'facility_app'@'127.0.0.1' IDENTIFIED BY 'CHANGE_ME';
CREATE USER IF NOT EXISTS 'facility_app'@'localhost'  IDENTIFIED BY 'CHANGE_ME';

-- App-level privileges only, scoped to this one database (no GRANT/admin rights).
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, REFERENCES
  ON facility_booking.* TO 'facility_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, REFERENCES
  ON facility_booking.* TO 'facility_app'@'localhost';

-- Test databases. `go test` with FB_TEST_MYSQL_DSN set creates a throwaway
-- database per test (fbtest_<random>) and drops it afterwards, which is how
-- packages stay isolated while `go test ./...` runs them in parallel. The
-- backslash escapes `_` so this matches only the fbtest_ prefix.
--
-- DEVELOPMENT ONLY. Do not run these two grants on the production database
-- server: they let the app user create and drop databases.
GRANT ALL PRIVILEGES ON `fbtest\_%`.* TO 'facility_app'@'127.0.0.1';
GRANT ALL PRIVILEGES ON `fbtest\_%`.* TO 'facility_app'@'localhost';

FLUSH PRIVILEGES;
