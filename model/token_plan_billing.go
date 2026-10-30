package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrTokenPlanUnavailable    = errors.New("token plan is unavailable or expired")
	ErrTokenPlanQuotaExhausted = errors.New("token plan has insufficient remaining tokens")
)

// TokenPlanPreConsumeRecord makes reservation, settlement and refund idempotent.
// The window indexes belong to request admission: a long-running request must
// never refund or consume another request's budget after a window rolls over.
type TokenPlanPreConsumeRecord struct {
	Id              int
	RequestId       string `gorm:"type:varchar(64);not null;uniqueIndex"`
	UserId          int    `gorm:"index"`
	UserTokenPlanId int    `gorm:"index"`
	Tokens          int64  `gorm:"type:bigint;not null"`
	ConsumedTokens  int64  `gorm:"type:bigint;not null;default:0"`
	Window1Index    int64  `gorm:"type:bigint;not null"`
	Window2Index    int64  `gorm:"type:bigint;not null"`
	UsageVersion    int64  `gorm:"type:bigint;not null"`
	Status          string `gorm:"type:varchar(16);not null"`
	CreatedAt       int64  `gorm:"type:bigint;not null"`
	UpdatedAt       int64  `gorm:"type:bigint;not null;index"`
}

// lockUserTokenPlan acquires a write lock before reading on SQLite, where
// SELECT FOR UPDATE is unavailable. Other dialects use the shared row lock.
func lockUserTokenPlan(tx *gorm.DB, userId, planId int) (*UserTokenPlan, error) {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := tx.Model(&UserTokenPlan{}).Where("id = ? AND user_id = ?", planId, userId).
			UpdateColumn("updated_time", gorm.Expr("updated_time")).Error; err != nil {
			return nil, err
		}
	}
	var plan UserTokenPlan
	err := lockForUpdate(tx).Where("id = ? AND user_id = ?", planId, userId).First(&plan).Error
	return &plan, err
}

// ReserveTokenPlanTokens reserves the maximum known request usage. For requests
// without an output bound, reserveRemaining conservatively holds the remaining
// budget until the upstream reports actual usage; callers can use max_tokens
// to allow bounded requests to share that budget concurrently.
func ReserveTokenPlanTokens(userId, planId int, requestId string, minimum int64, reserveRemaining bool) (*TokenPlanPreConsumeRecord, error) {
	if requestId == "" || minimum < 0 || minimum > common.MaxQuota {
		return nil, errors.New("invalid token plan reservation")
	}
	var record TokenPlanPreConsumeRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := lockUserTokenPlan(tx, userId, planId)
		if err != nil {
			return err
		}
		now := common.GetTimestamp()
		if plan.IsExpired(now) {
			return ErrTokenPlanUnavailable
		}
		err = tx.Where("request_id = ?", requestId).First(&record).Error
		if err == nil {
			if record.UserId != userId || record.UserTokenPlanId != planId || record.Status != "reserved" {
				return errors.New("token plan reservation is already finalized")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		anchor := plan.AnchorAt
		if anchor <= 0 {
			anchor = now
		}
		w1, w2 := plan.WindowViews(now)
		remaining := int64(common.MaxQuota)
		if plan.TotalTokens > 0 {
			remaining = min(remaining, plan.TotalTokens-plan.UsedTokens)
		}
		for _, window := range []TokenPlanWindowView{w1, w2} {
			if window.LimitTokens > 0 {
				remaining = min(remaining, window.LimitTokens-window.UsedTokens)
			}
		}
		amount := max(minimum, 1)
		if remaining < amount {
			return ErrTokenPlanQuotaExhausted
		}
		if reserveRemaining {
			amount = remaining
		}
		if plan.UsedTokens > common.MaxWalletQuota-amount {
			return ErrTokenPlanQuotaExhausted
		}
		record = TokenPlanPreConsumeRecord{
			RequestId: requestId, UserId: userId, UserTokenPlanId: planId, Tokens: amount,
			Window1Index: WindowIndex(anchor, now, plan.Window1Seconds),
			Window2Index: WindowIndex(anchor, now, plan.Window2Seconds),
			UsageVersion: plan.UsageVersion, Status: "reserved", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return tx.Model(plan).Updates(map[string]any{
			"anchor_at": anchor, "used_tokens": plan.UsedTokens + amount,
			"window1_index": record.Window1Index, "window1_used": w1.UsedTokens + amount,
			"window2_index": record.Window2Index, "window2_used": w2.UsedTokens + amount,
			"updated_time": now,
		}).Error
	})
	return &record, err
}

// SettleTokenPlanTokens replaces a reservation with actual usage exactly once.
// A refund uses zero and cannot undo a completed settlement. Administrative
// resets invalidate old reservations so their refunds cannot erase new usage.
func SettleTokenPlanTokens(userId, planId int, requestId string, actual int64, refund bool) error {
	if actual < 0 || actual > common.MaxQuota || requestId == "" {
		return errors.New("invalid token plan settlement")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		plan, err := lockUserTokenPlan(tx, userId, planId)
		if err != nil {
			return err
		}
		var record TokenPlanPreConsumeRecord
		if err := lockForUpdate(tx).Where("request_id = ? AND user_id = ? AND user_token_plan_id = ?", requestId, userId, planId).First(&record).Error; err != nil {
			return err
		}
		if record.Status != "reserved" {
			return nil
		}
		now := common.GetTimestamp()
		delta := actual - record.Tokens
		updates := map[string]any{"updated_time": now}
		if record.UsageVersion != plan.UsageVersion {
			delta = actual
			if actual > 0 {
				anchor := plan.AnchorAt
				if anchor <= 0 {
					anchor = now
				}
				w1, w2 := plan.WindowViews(now)
				updates["anchor_at"] = anchor
				updates["window1_index"], updates["window1_used"] = WindowIndex(anchor, now, plan.Window1Seconds), w1.UsedTokens+actual
				updates["window2_index"], updates["window2_used"] = WindowIndex(anchor, now, plan.Window2Seconds), w2.UsedTokens+actual
			}
		} else {
			if record.Window1Index == plan.Window1Index {
				updates["window1_used"] = max(plan.Window1Used+delta, 0)
			}
			if record.Window2Index == plan.Window2Index {
				updates["window2_used"] = max(plan.Window2Used+delta, 0)
			}
		}
		if delta > 0 && plan.UsedTokens > common.MaxWalletQuota-delta {
			return fmt.Errorf("token plan cumulative usage exceeds %d", common.MaxWalletQuota)
		}
		updates["used_tokens"] = max(plan.UsedTokens+delta, 0)
		if err := tx.Model(plan).Updates(updates).Error; err != nil {
			return err
		}
		status := "settled"
		if refund {
			status = "refunded"
		}
		return tx.Model(&record).Updates(map[string]any{"status": status, "consumed_tokens": actual, "updated_at": now}).Error
	})
}

func CleanupTokenPlanPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	cutoff := common.GetTimestamp() - max(olderThanSeconds, 7*24*3600)
	result := DB.Where("updated_at < ?", cutoff).Delete(&TokenPlanPreConsumeRecord{})
	return result.RowsAffected, result.Error
}
