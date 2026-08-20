// Package reports produces the utilization + revenue dashboard the municipality
// uses to plan and justify facility spending (§4.11). Metrics are computed for a
// selected period (month / quarter / year) with a comparison to the previous
// period of equal length.
package reports

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Period is the reporting window.
type Period string

const (
	Month   Period = "month"
	Quarter Period = "quarter"
	Year    Period = "year"
)

// Dashboard is the full reporting payload for the staff dashboard.
type Dashboard struct {
	Period      Period `json:"period"`
	PeriodLabel string `json:"periodLabel"`
	PrevLabel   string `json:"prevLabel"`

	RevenueCents      int64   `json:"revenueCents"`
	RevenueDeltaPct   float64 `json:"revenueDeltaPct"`
	Bookings          int64   `json:"bookings"`
	BookingsDeltaPct  float64 `json:"bookingsDeltaPct"`
	AvgUtilizationPct int     `json:"avgUtilizationPct"`
	Pending           int64   `json:"pending"`
	PendingOver24h    int64   `json:"pendingOver24h"`

	ByFacility  []FacilityCount `json:"byFacility"`
	TopSpaces   []SpaceRevenue  `json:"topSpaces"`
	Trend       []TrendPoint    `json:"trend"`
	ResidentPct int             `json:"residentPct"`
}

// FacilityCount is a confirmed-booking count for a facility (the bars).
type FacilityCount struct {
	FacilityName string `json:"facilityName"`
	Bookings     int64  `json:"bookings"`
}

// SpaceRevenue is a facility's revenue and utilization (the top-spaces table).
type SpaceRevenue struct {
	FacilityName   string `json:"facilityName"`
	RevenueCents   int64  `json:"revenueCents"`
	UtilizationPct int    `json:"utilizationPct"`
}

// TrendPoint is one month's utilization (the trend line).
type TrendPoint struct {
	Label          string `json:"label"`
	UtilizationPct int    `json:"utilizationPct"`
}

// Service computes reports from the database.
type Service struct{ db *gorm.DB }

// NewService constructs the reports service.
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Summarize builds the dashboard for the given period.
func (s *Service) Summarize(ctx context.Context, period Period) (Dashboard, error) {
	now := time.Now()
	start, prevStart := windowStarts(now, period)
	d := Dashboard{
		Period: period, PeriodLabel: periodLabel(now, period), PrevLabel: prevPeriodLabel(period),
	}

	// Bookings (confirmed) this period and last, for the delta.
	cur, err := s.confirmedCount(ctx, start, now)
	if err != nil {
		return d, err
	}
	prev, err := s.confirmedCount(ctx, prevStart, start)
	if err != nil {
		return d, err
	}
	d.Bookings = cur
	d.BookingsDeltaPct = deltaPct(cur, prev)

	// Revenue (paid) this period and last.
	curRev, err := s.revenue(ctx, start, now)
	if err != nil {
		return d, err
	}
	prevRev, err := s.revenue(ctx, prevStart, start)
	if err != nil {
		return d, err
	}
	d.RevenueCents = curRev
	d.RevenueDeltaPct = deltaPct(curRev, prevRev)

	// Pending (current, not period-bound) + how many are older than 24h.
	if err := s.db.WithContext(ctx).Model(&domain.Booking{}).Where("status = ?", domain.StatusPending).Count(&d.Pending).Error; err != nil {
		return d, err
	}
	if err := s.db.WithContext(ctx).Model(&domain.Booking{}).
		Where("status = ? AND created_at < ?", domain.StatusPending, now.Add(-24*time.Hour)).
		Count(&d.PendingOver24h).Error; err != nil {
		return d, err
	}

	if d.ByFacility, err = s.byFacility(ctx, start, now); err != nil {
		return d, err
	}
	if d.TopSpaces, err = s.topSpaces(ctx, start, now); err != nil {
		return d, err
	}
	if d.AvgUtilizationPct, err = s.avgUtilization(ctx, start, now); err != nil {
		return d, err
	}
	if d.ResidentPct, err = s.residentPct(ctx, start, now); err != nil {
		return d, err
	}
	if d.Trend, err = s.trend(ctx, now); err != nil {
		return d, err
	}
	return d, nil
}

func (s *Service) confirmedCount(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&domain.Booking{}).
		Where("status = ? AND starts_at >= ? AND starts_at < ?", domain.StatusConfirmed, from, to).
		Count(&n).Error
	return n, err
}

// revenue sums paid payments whose booking occurs in [from, to).
func (s *Service) revenue(ctx context.Context, from, to time.Time) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Model(&domain.Payment{}).
		Joins("JOIN bookings ON bookings.id = payments.booking_id").
		Where("payments.status = ? AND bookings.starts_at >= ? AND bookings.starts_at < ?", domain.PayPaid, from, to).
		Select("COALESCE(SUM(payments.amount_cents),0)").Scan(&total).Error
	return total, err
}

func (s *Service) byFacility(ctx context.Context, from, to time.Time) ([]FacilityCount, error) {
	var rows []FacilityCount
	err := s.db.WithContext(ctx).Model(&domain.Booking{}).
		Select("facilities.name as facility_name, COUNT(bookings.id) as bookings").
		Joins("JOIN facilities ON facilities.id = bookings.facility_id").
		Where("bookings.status = ? AND bookings.starts_at >= ? AND bookings.starts_at < ?", domain.StatusConfirmed, from, to).
		Group("facilities.id, facilities.name").Order("bookings desc").Scan(&rows).Error
	return rows, err
}

func (s *Service) topSpaces(ctx context.Context, from, to time.Time) ([]SpaceRevenue, error) {
	// Revenue per facility (paid), top 6.
	var rows []SpaceRevenue
	err := s.db.WithContext(ctx).Model(&domain.Payment{}).
		Select("facilities.name as facility_name, COALESCE(SUM(payments.amount_cents),0) as revenue_cents").
		Joins("JOIN bookings ON bookings.id = payments.booking_id").
		Joins("JOIN facilities ON facilities.id = bookings.facility_id").
		Where("payments.status = ? AND bookings.starts_at >= ? AND bookings.starts_at < ?", domain.PayPaid, from, to).
		Group("facilities.id, facilities.name").Order("revenue_cents desc").Limit(6).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	// Attach each facility's day-utilization.
	util, err := s.utilizationByFacility(ctx, from, to)
	if err != nil {
		return nil, err
	}
	openDays := dayCount(from, to)
	for i := range rows {
		rows[i].UtilizationPct = pct(util[rows[i].FacilityName], openDays)
	}
	return rows, nil
}

// utilizationByFacility returns, per facility name, the number of distinct days
// with at least one confirmed booking in [from, to).
func (s *Service) utilizationByFacility(ctx context.Context, from, to time.Time) (map[string]int, error) {
	type row struct {
		FacilityName string
		Days         int
	}
	var rows []row
	err := s.db.WithContext(ctx).Model(&domain.Booking{}).
		Select("facilities.name as facility_name, COUNT(DISTINCT date(bookings.starts_at)) as days").
		Joins("JOIN facilities ON facilities.id = bookings.facility_id").
		Where("bookings.status = ? AND bookings.starts_at >= ? AND bookings.starts_at < ?", domain.StatusConfirmed, from, to).
		Group("facilities.id, facilities.name").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.FacilityName] = r.Days
	}
	return out, nil
}

// avgUtilization is total booked facility-days / (facilities × open days).
func (s *Service) avgUtilization(ctx context.Context, from, to time.Time) (int, error) {
	util, err := s.utilizationByFacility(ctx, from, to)
	if err != nil {
		return 0, err
	}
	var facilities int64
	if err := s.db.WithContext(ctx).Model(&domain.Facility{}).Count(&facilities).Error; err != nil {
		return 0, err
	}
	if facilities == 0 {
		return 0, nil
	}
	total := 0
	for _, d := range util {
		total += d
	}
	return pct(total, dayCount(from, to)*int(facilities)), nil
}

func (s *Service) residentPct(ctx context.Context, from, to time.Time) (int, error) {
	base := s.db.WithContext(ctx).Model(&domain.Booking{}).
		Where("status = ? AND starts_at >= ? AND starts_at < ?", domain.StatusConfirmed, from, to)
	var total, resident int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return 0, err
	}
	if err := base.Session(&gorm.Session{}).Where("resident = ?", true).Count(&resident).Error; err != nil {
		return 0, err
	}
	return pct(int(resident), int(total)), nil
}

// trend returns day-utilization for each of the last 6 months.
func (s *Service) trend(ctx context.Context, now time.Time) ([]TrendPoint, error) {
	var facilities int64
	if err := s.db.WithContext(ctx).Model(&domain.Facility{}).Count(&facilities).Error; err != nil {
		return nil, err
	}
	out := make([]TrendPoint, 0, 6)
	for i := 5; i >= 0; i-- {
		mStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -i, 0)
		mEnd := mStart.AddDate(0, 1, 0)
		if mEnd.After(now) {
			mEnd = now
		}
		util, err := s.utilizationByFacility(ctx, mStart, mEnd)
		if err != nil {
			return nil, err
		}
		total := 0
		for _, d := range util {
			total += d
		}
		out = append(out, TrendPoint{
			Label:          mStart.Format("Jan"),
			UtilizationPct: pct(total, dayCount(mStart, mEnd)*int(facilities)),
		})
	}
	return out, nil
}

// --- helpers ---------------------------------------------------------------

func windowStarts(now time.Time, p Period) (start, prevStart time.Time) {
	switch p {
	case Month:
		start = now.AddDate(0, -1, 0)
	case Quarter:
		start = now.AddDate(0, -3, 0)
	default: // year
		start = now.AddDate(-1, 0, 0)
	}
	prevStart = start.Add(-now.Sub(start))
	return start, prevStart
}

func periodLabel(now time.Time, p Period) string {
	switch p {
	case Month:
		return now.Format("Jan 2006")
	case Quarter:
		return fmt.Sprintf("Q%d %d", (int(now.Month())-1)/3+1, now.Year())
	default:
		return "Last 12 months"
	}
}

func prevPeriodLabel(p Period) string {
	switch p {
	case Month:
		return "vs prev month"
	case Quarter:
		return "vs prev quarter"
	default:
		return "vs prev year"
	}
}

func deltaPct(cur, prev int64) float64 {
	if prev == 0 {
		if cur == 0 {
			return 0
		}
		return 100
	}
	return float64(cur-prev) / float64(prev) * 100
}

func pct(n, of int) int {
	if of <= 0 {
		return 0
	}
	return int(float64(n) / float64(of) * 100)
}

// dayCount is the number of whole days in [from, to), at least 1.
func dayCount(from, to time.Time) int {
	d := int(to.Sub(from).Hours()/24) + 1
	if d < 1 {
		return 1
	}
	return d
}
