package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

var (
	// ErrEmptyCondition rejects a conditional approval that imposes nothing.
	// Staff meaning "approve" should approve: a condition set with no terms, no
	// fee and no document would park the booking short of confirmed forever
	// while telling the resident nothing to do about it.
	ErrEmptyCondition = errors.New("booking: a conditional approval must impose at least one condition")
	// ErrFeeAlreadyPaid rejects adding a fee to a booking that is already paid
	// in full. Staff need to know, because the resident must be asked for more
	// money and that is a conversation, not a silent repricing.
	ErrFeeAlreadyPaid = errors.New("booking: this booking is already paid; adding a fee needs a separate charge")
	// ErrNoConditions is returned when acting on conditions that do not exist.
	ErrNoConditions = errors.New("booking: this booking has no conditions")
)

// ConditionInput is what staff impose when approving with conditions.
type ConditionInput struct {
	Terms              string
	AdditionalFeeCents int
	DocumentLabel      string
}

// empty reports whether this imposes nothing at all.
func (c ConditionInput) empty() bool {
	return strings.TrimSpace(c.Terms) == "" && c.AdditionalFeeCents <= 0 && strings.TrimSpace(c.DocumentLabel) == ""
}

// ApproveWithConditions moves a pending booking to conditional (§4.5, §4.8).
//
// The booking is NOT confirmed: it sits holding its slot while the resident
// accepts the terms, pays any added fee and uploads any required document.
// Holding the slot is the point — releasing it would sell the space out from
// under someone who is busy satisfying the conditions staff just set.
//
// An added fee is folded into Booking.FeeCents inside the same transaction, so
// every existing pricing, payment and reporting path keeps working without
// learning that conditions exist.
func (s *Service) ApproveWithConditions(ctx context.Context, actorID, bookingID string, in ConditionInput) (*domain.Booking, error) {
	if in.empty() {
		return nil, ErrEmptyCondition
	}
	var b domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		// Only a pending request can be approved, conditionally or otherwise.
		// Re-imposing conditions on an already-conditional booking is allowed so
		// staff can correct a mistake before the resident acts.
		if b.Status != domain.StatusPending && b.Status != domain.StatusConditional {
			return ErrBadState
		}
		if in.AdditionalFeeCents > 0 {
			if err := guardUnpaid(tx, b); err != nil {
				return err
			}
		}

		// Replace any previous set: the conditions are the current contract, and
		// the audit log carries the history.
		//
		// Unscoped, because Base carries DeletedAt: a soft delete leaves the row
		// in place, the unique index on booking_id still sees it, and the next
		// approval fails with a duplicate key. A superseded condition set has no
		// value to keep — the audit trail is the history.
		if err := tx.Unscoped().Where("booking_id = ?", b.ID).Delete(&domain.BookingCondition{}).Error; err != nil {
			return err
		}
		cond := domain.BookingCondition{
			BookingID:          b.ID,
			Terms:              strings.TrimSpace(in.Terms),
			AdditionalFeeCents: in.AdditionalFeeCents,
			DocumentLabel:      strings.TrimSpace(in.DocumentLabel),
			SetByID:            actorID,
		}
		if err := tx.Create(&cond).Error; err != nil {
			return err
		}

		updates := map[string]any{"status": domain.StatusConditional}
		if in.AdditionalFeeCents > 0 {
			updates["fee_cents"] = b.FeeCents + in.AdditionalFeeCents
		}
		if err := tx.Model(&b).Updates(updates).Error; err != nil {
			return err
		}
		b.Status = domain.StatusConditional
		b.FeeCents += in.AdditionalFeeCents
		b.Condition = &cond

		return writeAudit(tx, actorID, "booking.approve.conditional", b.ID)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// guardUnpaid refuses to raise the fee on a booking already paid in full.
//
// Silently repricing a paid booking would leave the resident owing money they
// were never asked for, and the confirmation gate would then hold their booking
// hostage to a debt nobody told them about.
func guardUnpaid(tx *gorm.DB, b domain.Booking) error {
	// Find, not First: an unpaid booking is the normal case, and First would log
	// a record-not-found for every one of them.
	var rows []domain.Payment
	if err := tx.Limit(1).Find(&rows, "booking_id = ?", b.ID).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if pay := rows[0]; pay.Status == domain.PayPaid && pay.AmountCents >= b.FeeCents {
		return ErrFeeAlreadyPaid
	}
	return nil
}

// AcceptConditions records the resident agreeing to the terms (§4.5).
//
// Agreement alone does not confirm: a fee may still be owed and a document may
// still be missing. Confirmation is decided by Outstanding, in one place, so
// there is no path that confirms a booking with a condition still open.
func (s *Service) AcceptConditions(ctx context.Context, actor *domain.User, bookingID string) (*domain.Booking, error) {
	var b domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Preload("Condition").First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		// The booker accepts their own conditions. Staff cannot accept on their
		// behalf — the acceptance is the resident's agreement, and one recorded
		// by the person who imposed it is worth nothing.
		if b.UserID != actor.ID {
			return ErrForbidden
		}
		if b.Status != domain.StatusConditional || b.Condition == nil {
			return ErrNoConditions
		}
		if b.Condition.Accepted() {
			return nil // idempotent: agreeing twice is agreeing once
		}
		now := time.Now()
		if err := tx.Model(b.Condition).Update("accepted_at", now).Error; err != nil {
			return err
		}
		b.Condition.AcceptedAt = &now
		return writeAudit(tx, actor.ID, "booking.conditions.accepted", b.ID)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Outstanding is what still stands between a conditional booking and
// confirmation, in the resident's words.
//
// Returned as a list rather than a boolean because §4.5 requires the resident be
// told *exactly* what is outstanding — "not confirmed yet" is not an answer
// anyone can act on.
type Outstanding struct {
	AcceptTerms  bool   `json:"acceptTerms"`
	PayCents     int    `json:"payCents"`
	UploadLabel  string `json:"uploadLabel"`
	AllSatisfied bool   `json:"allSatisfied"`
}

// WhatIsOutstanding computes what a conditional booking is still waiting on.
//
// hasDocument is supplied by the caller rather than looked up here, because the
// document lives behind internal/waiver's access rules and this package has no
// business reaching around them.
func WhatIsOutstanding(b domain.Booking, pay *domain.Payment, hasDocument bool) Outstanding {
	var out Outstanding
	if b.Condition == nil {
		out.AllSatisfied = true
		return out
	}
	out.AcceptTerms = b.Condition.Terms != "" && !b.Condition.Accepted()

	paid := 0
	if pay != nil && pay.Status == domain.PayPaid {
		paid = pay.AmountCents
	}
	if owed := b.FeeCents - paid; owed > 0 {
		out.PayCents = owed
	}

	if b.Condition.RequiresDocument() && !hasDocument {
		out.UploadLabel = b.Condition.DocumentLabel
	}

	out.AllSatisfied = !out.AcceptTerms && out.PayCents == 0 && out.UploadLabel == ""
	return out
}

// ConfirmIfSatisfied moves a conditional booking to confirmed once nothing is
// outstanding, and reports whether it did.
//
// Deliberately the only way out of conditional: every route that could confirm
// one — accepting terms, a payment settling, a document arriving — calls this,
// so the gate cannot be bypassed by adding a fourth.
func (s *Service) ConfirmIfSatisfied(ctx context.Context, bookingID string, hasDocument bool) (*domain.Booking, bool, error) {
	var b domain.Booking
	confirmed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Preload("Condition").Preload("Payment").First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		if b.Status != domain.StatusConditional {
			return nil // nothing to do; not an error
		}
		if !WhatIsOutstanding(b, b.Payment, hasDocument).AllSatisfied {
			return nil
		}
		if err := tx.Model(&b).Update("status", domain.StatusConfirmed).Error; err != nil {
			return err
		}
		b.Status = domain.StatusConfirmed
		confirmed = true
		// No actor: the resident satisfied the last condition, but the decision
		// to confirm is the system applying the terms staff already set.
		return writeAudit(tx, "", "booking.conditions.satisfied", b.ID)
	})
	if err != nil {
		return nil, false, err
	}
	return &b, confirmed, nil
}

// AwaitingResident lists conditionally-approved bookings still waiting on the
// resident — the staff view §4.8 asks for.
func (s *Service) AwaitingResident(ctx context.Context) ([]domain.Booking, error) {
	out := []domain.Booking{}
	err := s.db.WithContext(ctx).
		Preload("Facility").Preload("User").Preload("Condition").Preload("Payment").
		Where("status = ?", domain.StatusConditional).
		Order("starts_at asc").Find(&out).Error
	return out, err
}

// DescribeConditions is the audit line a person reads months later.
func DescribeConditions(in ConditionInput) string {
	parts := []string{}
	if t := strings.TrimSpace(in.Terms); t != "" {
		parts = append(parts, "terms: "+t)
	}
	if in.AdditionalFeeCents > 0 {
		parts = append(parts, fmt.Sprintf("additional fee: %d cents", in.AdditionalFeeCents))
	}
	if d := strings.TrimSpace(in.DocumentLabel); d != "" {
		parts = append(parts, "document required: "+d)
	}
	return "Staff approved with conditions — " + strings.Join(parts, "; ")
}
