package payment

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Window is an inclusive day range for scoping the ledger. A zero From or To is
// unbounded on that side, so a zero Window means the whole ledger.
type Window struct {
	From time.Time // inclusive start-of-day; zero = no lower bound
	To   time.Time // inclusive day; zero = no upper bound
}

// apply adds the created_at bounds to a query. col is the qualified column name
// (e.g. "created_at" or "t.created_at"). The upper bound is exclusive of the day
// after To so the whole To day is included.
func (w Window) apply(q *gorm.DB, col string) *gorm.DB {
	if !w.From.IsZero() {
		q = q.Where(col+" >= ?", w.From)
	}
	if !w.To.IsZero() {
		q = q.Where(col+" < ?", w.To.AddDate(0, 0, 1))
	}
	return q
}

// Params is a paged, filtered request for the ledger.
type Params struct {
	Window
	Filter   string // "", "all", "succeeded", "failed", "refunds"
	Page     int    // 0-based
	PageSize int    // rows per page
}

// applyFilter narrows a query to a status chip. prefix qualifies the columns
// ("" for the bare table, "t." when aliased). An unknown filter is treated as
// "all" (no narrowing).
func applyFilter(q *gorm.DB, filter, prefix string) *gorm.DB {
	switch filter {
	case "succeeded":
		return q.Where(prefix+"kind = ? AND "+prefix+"status = ?", domain.TxnCharge, domain.TxnSucceeded)
	case "failed":
		return q.Where(prefix+"status = ?", domain.TxnFailed)
	case "refunds":
		return q.Where(prefix+"kind = ?", domain.TxnRefund)
	default:
		return q
	}
}

// TxnRow is a ledger entry joined to the booking context staff need to reconcile
// it — which space, which resident.
type TxnRow struct {
	domain.PaymentTransaction
	FacilityName string `json:"facilityName"`
	UserName     string `json:"userName"`
	UserEmail    string `json:"userEmail"`
}

// Summary aggregates the whole ledger (not just the returned page) so the totals
// are correct regardless of the list limit.
type Summary struct {
	CollectedCents int `json:"collectedCents"` // succeeded charges
	RefundedCents  int `json:"refundedCents"`  // succeeded refunds
	NetCents       int `json:"netCents"`       // collected − refunded
	Succeeded      int `json:"succeeded"`      // # successful charges
	Failed         int `json:"failed"`         // # declined charges
	Refunds        int `json:"refunds"`        // # refunds
}

// Reconciliation is the payload for the admin payments screen. Summary covers the
// whole window; Total is the count matching the window + filter (for paging);
// Transactions is the requested page of that set.
type Reconciliation struct {
	Summary      Summary  `json:"summary"`
	Transactions []TxnRow `json:"transactions"`
	Total        int64    `json:"total"`
	Page         int      `json:"page"`
	PageSize     int      `json:"pageSize"`
}

// Reconcile returns the window's totals plus one page of matching transactions,
// newest first. The Summary is filter-independent (it always describes the whole
// window's money), while Total and the page respect the status filter.
func (s *Service) Reconcile(ctx context.Context, p Params) (Reconciliation, error) {
	if p.PageSize <= 0 {
		p.PageSize = 25
	}
	if p.Page < 0 {
		p.Page = 0
	}
	sum, err := s.summarize(ctx, p.Window)
	if err != nil {
		return Reconciliation{}, err
	}
	total, err := s.count(ctx, p.Window, p.Filter)
	if err != nil {
		return Reconciliation{}, err
	}
	rows, err := s.transactions(ctx, p)
	if err != nil {
		return Reconciliation{}, err
	}
	return Reconciliation{Summary: sum, Transactions: rows, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// count is the number of transactions matching the window + filter — the row the
// pager needs to size itself.
func (s *Service) count(ctx context.Context, w Window, filter string) (int64, error) {
	var n int64
	q := s.db.WithContext(ctx).Model(&domain.PaymentTransaction{}).Where("deleted_at IS NULL")
	q = w.apply(q, "created_at")
	q = applyFilter(q, filter, "")
	err := q.Count(&n).Error
	return n, err
}

func (s *Service) transactions(ctx context.Context, p Params) ([]TxnRow, error) {
	rows := []TxnRow{}
	q := s.db.WithContext(ctx).
		Table("payment_transactions AS t").
		Select("t.*, f.name AS facility_name, u.name AS user_name, u.email AS user_email").
		Joins("LEFT JOIN bookings b ON b.id = t.booking_id").
		Joins("LEFT JOIN facilities f ON f.id = b.facility_id").
		Joins("LEFT JOIN users u ON u.id = b.user_id").
		Where("t.deleted_at IS NULL")
	q = p.Window.apply(q, "t.created_at")
	q = applyFilter(q, p.Filter, "t.")
	err := q.
		Order("t.created_at DESC").
		Limit(p.PageSize).
		Offset(p.Page * p.PageSize).
		Scan(&rows).Error
	return rows, err
}

// summarize folds the ledger into totals with one grouped query.
func (s *Service) summarize(ctx context.Context, w Window) (Summary, error) {
	var agg []struct {
		Kind   domain.TxnKind
		Status domain.TxnStatus
		Cnt    int
		Sum    int
	}
	q := s.db.WithContext(ctx).
		Table("payment_transactions").
		Select("kind, status, COUNT(*) AS cnt, COALESCE(SUM(amount_cents), 0) AS sum").
		Where("deleted_at IS NULL")
	err := w.apply(q, "created_at").
		Group("kind, status").
		Scan(&agg).Error
	if err != nil {
		return Summary{}, err
	}

	var sum Summary
	for _, a := range agg {
		switch {
		case a.Kind == domain.TxnCharge && a.Status == domain.TxnSucceeded:
			sum.CollectedCents += a.Sum
			sum.Succeeded += a.Cnt
		case a.Kind == domain.TxnCharge && a.Status == domain.TxnFailed:
			sum.Failed += a.Cnt
		case a.Kind == domain.TxnRefund && a.Status == domain.TxnSucceeded:
			sum.RefundedCents += a.Sum
			sum.Refunds += a.Cnt
		}
	}
	sum.NetCents = sum.CollectedCents - sum.RefundedCents
	return sum, nil
}
