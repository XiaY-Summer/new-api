package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const TokenPlanOrderPaidPending = "paid_pending"

var ErrTokenPlanFulfillmentPending = errors.New("TOKEN PLAN payment received; activation is pending")

// TokenPlanOrder keeps paid terms independent from later template edits.
type TokenPlanOrder struct {
	Id               int     `json:"id"`
	UserId           int     `json:"user_id" gorm:"index"`
	PlanId           int     `json:"plan_id" gorm:"index"`
	Money            float64 `json:"money"`
	TradeNo          string  `json:"trade_no" gorm:"uniqueIndex;type:varchar(255)"`
	PaymentMethod    string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider  string  `json:"payment_provider" gorm:"type:varchar(50)"`
	Status           string  `json:"status" gorm:"type:varchar(20);index"`
	CreateTime       int64   `json:"create_time"`
	CompleteTime     int64   `json:"complete_time"`
	PaidTime         int64   `json:"paid_time" gorm:"not null;default:0"`
	FulfillmentError string  `json:"fulfillment_error" gorm:"type:text"`
	PlanSnapshot     string  `json:"-" gorm:"type:text"`
	ProviderPayload  string  `json:"-" gorm:"type:text"`
}

func CreateTokenPlanOrder(userId, planId int, tradeNo, paymentMethod string) (*TokenPlanOrder, *TokenPlan, error) {
	var order TokenPlanOrder
	var plan TokenPlan
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTokenPlanOrderUser(tx, userId); err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ? AND enabled = ?", planId, true).First(&plan).Error; err != nil {
			return common.NewMessage("Subscription plan is not enabled")
		}
		plan.NormalizeDefaults()
		if math.IsNaN(plan.PriceAmount) || math.IsInf(plan.PriceAmount, 0) || plan.PriceAmount < 0.01 {
			return common.NewMessage("Plan amount is too low")
		}
		if plan.Currency != "CNY" && plan.Currency != "RMB" {
			return common.NewMessage("EPay only supports CNY plans")
		}
		if !decimal.NewFromFloat(plan.PriceAmount).Equal(decimal.NewFromFloat(plan.PriceAmount).Round(2)) {
			return common.NewMessage("EPay plan prices must use at most two decimal places")
		}
		if plan.TotalTokens < 0 || plan.TotalTokens > common.MaxWalletQuota || plan.DurationDays > 36500 {
			return common.NewMessage("Invalid parameters")
		}
		var current UserTokenPlan
		err := tx.Where("user_id = ? AND status = ?", userId, TokenPlanStatusActive).Order("id desc").First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if current.PlanId == plan.Id && current.TotalTokens > 0 && plan.TotalTokens > 0 && current.TotalTokens > common.MaxWalletQuota-plan.TotalTokens {
				return common.NewMessage("Invalid parameters")
			}
			if current.PlanId != plan.Id && current.EndTime > common.GetTimestamp() {
				var currentPlan TokenPlan
				if err := tx.First(&currentPlan, current.PlanId).Error; err != nil {
					return err
				}
				if tokenPlanQuotaDowngrade(currentPlan.TotalTokens, plan.TotalTokens) {
					return common.NewMessage("TOKEN PLAN downgrades are not supported while the current plan is active")
				}
			}
		}
		snapshot, err := common.Marshal(plan)
		if err != nil {
			return err
		}
		money, _ := decimal.NewFromFloat(plan.PriceAmount).Round(2).Float64()
		order = TokenPlanOrder{
			UserId: userId, PlanId: plan.Id, Money: money, TradeNo: tradeNo,
			PaymentMethod: paymentMethod, PaymentProvider: PaymentProviderEpay,
			Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(), PlanSnapshot: string(snapshot),
		}
		return tx.Create(&order).Error
	})
	return &order, &plan, err
}

// CompleteTokenPlanOrder commits verified payment evidence before attempting
// fulfillment. A failed grant must never make a paid order look unpaid.
func CompleteTokenPlanOrder(tradeNo, money, paymentMethod, providerPayload string) error {
	paid, err := decimal.NewFromString(money)
	if err != nil || !paid.IsPositive() || strings.TrimSpace(tradeNo) == "" {
		return errors.New("invalid token plan payment")
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		// Acquire a write lock before the first read, including on SQLite.
		if err := tx.Model(&TokenPlanOrder{}).Where("trade_no = ?", tradeNo).
			UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		var order TokenPlanOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderEpay || order.PaymentMethod != paymentMethod {
			return ErrPaymentMethodMismatch
		}
		if !paid.Equal(decimal.NewFromFloat(order.Money).Round(2)) {
			return errors.New("token plan payment amount mismatch")
		}
		if order.Status == common.TopUpStatusSuccess || order.Status == TokenPlanOrderPaidPending {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return errors.New("token plan order status invalid")
		}
		return tx.Model(&order).Updates(map[string]any{
			"status": TokenPlanOrderPaidPending, "paid_time": common.GetTimestamp(),
			"provider_payload": providerPayload,
		}).Error
	})
	if err != nil {
		return err
	}
	if err := FulfillTokenPlanOrder(tradeNo); err != nil {
		return fmt.Errorf("%w: %v", ErrTokenPlanFulfillmentPending, err)
	}
	return nil
}

// FulfillTokenPlanOrder retries only recorded payments. Grant, receipt and final
// order status are atomic and locked across callbacks and administrator retries.
func FulfillTokenPlanOrder(tradeNo string) error {
	var completed *TokenPlanOrder
	var title string
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&TokenPlanOrder{}).Where("trade_no = ?", tradeNo).
			UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		var order TokenPlanOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return err
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if order.Status != TokenPlanOrderPaidPending {
			return errors.New("token plan order has not been paid")
		}
		var plan TokenPlan
		if err := common.UnmarshalJsonStr(order.PlanSnapshot, &plan); err != nil {
			return err
		}
		if plan.Id != order.PlanId {
			return errors.New("token plan order snapshot mismatch")
		}
		if err := lockTokenPlanOrderUser(tx, order.UserId); err != nil {
			return err
		}
		// A deleted template cancels its subscriptions; it must also block new grants.
		var template TokenPlan
		if err := lockForUpdate(tx).First(&template, order.PlanId).Error; err != nil {
			return err
		}
		if _, _, err := purchaseTokenPlanTx(tx, order.UserId, &plan); err != nil {
			return err
		}
		now := common.GetTimestamp()
		if err := tx.Create(&TopUp{
			UserId: order.UserId, Amount: 0, Money: order.Money, TradeNo: order.TradeNo,
			PaymentMethod: order.PaymentMethod, PaymentProvider: PaymentProviderEpay,
			CreateTime: order.CreateTime, CompleteTime: order.PaidTime, Status: common.TopUpStatusSuccess,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Updates(map[string]any{
			"status": common.TopUpStatusSuccess, "complete_time": now, "fulfillment_error": "",
		}).Error; err != nil {
			return err
		}
		completed, title = &order, plan.Title
		return nil
	})
	if err != nil {
		// A concurrent successful retry must not be overwritten by this failure.
		detail := []rune(err.Error())
		saveErr := DB.Model(&TokenPlanOrder{}).
			Where("trade_no = ? AND status = ?", tradeNo, TokenPlanOrderPaidPending).
			UpdateColumn("fulfillment_error", string(detail[:min(len(detail), 2000)])).Error
		return errors.Join(err, saveErr)
	}
	if err == nil && completed != nil {
		RecordLog(completed.UserId, LogTypeTopup, common.NewMessage("Subscription purchased, plan: {{plan}}, payment amount: {{amount}}, payment method: {{method}}", map[string]any{
			"plan": "TOKEN PLAN: " + title, "amount": fmt.Sprintf("%.2f", completed.Money), "method": completed.PaymentMethod,
		}))
	}
	return err
}

func CloseUnpaidTokenPlanOrder(tradeNo string) error {
	return DB.Model(&TokenPlanOrder{}).
		Where("trade_no = ? AND status = ?", tradeNo, common.TopUpStatusPending).
		UpdateColumn("status", common.TopUpStatusExpired).Error
}

func ListPaidPendingTokenPlanOrders(userId, offset, limit int) ([]*TokenPlanOrder, int64, error) {
	query := DB.Model(&TokenPlanOrder{}).Where("status = ?", TokenPlanOrderPaidPending)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	orders := make([]*TokenPlanOrder, 0)
	err := query.Order("id desc").Offset(max(offset, 0)).Limit(min(max(limit, 1), 100)).Find(&orders).Error
	return orders, total, err
}

func lockTokenPlanOrderUser(tx *gorm.DB, userId int) error {
	if err := tx.Model(&User{}).Where("id = ?", userId).UpdateColumn("quota", gorm.Expr("quota")).Error; err != nil {
		return err
	}
	var user User
	return lockForUpdate(tx).Select("id").Where("id = ? AND status = ?", userId, common.UserStatusEnabled).First(&user).Error
}
