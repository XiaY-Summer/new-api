package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ---- 用户侧 ----

// GetTokenPlanPlans 返回上架中的 TOKEN PLAN 套餐列表。
func GetTokenPlanPlans(c *gin.Context) {
	plans, err := model.GetEnabledTokenPlans()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, p := range plans {
		p.NormalizeDefaults()
	}
	common.ApiSuccess(c, plans)
}

// GetTokenPlanSelf 返回当前用户套餐概览：两个窗口的用量、专用 key、可用模型。
func GetTokenPlanSelf(c *gin.Context) {
	userId := c.GetInt("id")
	sub, err := model.GetActiveUserTokenPlan(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if sub == nil {
		common.ApiSuccess(c, gin.H{"has_plan": false})
		return
	}

	now := common.GetTimestamp()
	window1, window2 := sub.WindowViews(now)

	keyInfo := gin.H{}
	if sub.TokenId > 0 {
		if token, err := model.GetTokenByIds(sub.TokenId, userId); err == nil && token != nil {
			keyInfo = gin.H{
				"id":           token.Id,
				"key":          token.Key,
				"name":         token.Name,
				"created_time": token.CreatedTime,
			}
		}
	}

	remaining := sub.TotalTokens - sub.UsedTokens
	if remaining < 0 {
		remaining = 0
	}
	percent := 0
	if sub.TotalTokens > 0 {
		percent = int(sub.UsedTokens * 100 / sub.TotalTokens)
		if percent > 100 {
			percent = 100
		}
	}

	models := model.SplitModelLimits(sub.ModelLimits)
	unrestricted := len(models) == 0
	if unrestricted {
		models = model.GetEnabledModels()
	}

	common.ApiSuccess(c, gin.H{
		"has_plan": true,
		"subscription": gin.H{
			"id":               sub.Id,
			"plan_id":          sub.PlanId,
			"plan_title":       sub.PlanTitle,
			"status":           sub.Status,
			"start_time":       sub.StartTime,
			"end_time":         sub.EndTime,
			"total_tokens":     sub.TotalTokens,
			"used_tokens":      sub.UsedTokens,
			"remaining_tokens": remaining,
			"used_percent":     percent,
			"anchor_at":        sub.AnchorAt,
			"metering_mode":    sub.MeteringMode,
		},
		"windows":      []model.TokenPlanWindowView{window1, window2},
		"key":          keyInfo,
		"models":       models,
		"unrestricted": unrestricted,
	})
}

type TokenPlanPurchaseRequest struct {
	PlanId int `json:"plan_id"`
}

// PurchaseTokenPlan 是占位版购买流程：不接支付，直接开通套餐。
func PurchaseTokenPlan(c *gin.Context) {
	userId := c.GetInt("id")
	var req TokenPlanPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	sub, token, err := model.PurchaseTokenPlan(userId, req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"subscription": sub,
		"key":          token.Key,
	})
}

// ResetTokenPlanKey 重置专用密钥（原地换串）。
func ResetTokenPlanKey(c *gin.Context) {
	userId := c.GetInt("id")
	token, err := model.ResetTokenPlanKey(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"key": token.Key})
}

// ---- 管理侧 ----

func AdminListTokenPlans(c *gin.Context) {
	plans, err := model.GetAllTokenPlans()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for _, p := range plans {
		p.NormalizeDefaults()
	}
	common.ApiSuccess(c, plans)
}

type AdminUpsertTokenPlanRequest struct {
	Plan model.TokenPlan `json:"plan"`
}

func validateTokenPlan(plan *model.TokenPlan) string {
	if strings.TrimSpace(plan.Title) == "" {
		return "Plan title cannot be empty"
	}
	if plan.PriceAmount < 0 {
		return "Price cannot be negative"
	}
	if plan.DurationDays <= 0 {
		return "Duration days must be greater than 0"
	}
	if plan.TotalTokens < 0 {
		return "Total tokens cannot be negative"
	}
	if plan.Window1Seconds <= 0 || plan.Window2Seconds <= 0 {
		return "Window duration must be greater than 0"
	}
	if plan.Window1Tokens < 0 || plan.Window2Tokens < 0 {
		return "Window tokens cannot be negative"
	}
	if plan.MeteringMode != model.TokenPlanMeteringRaw {
		plan.MeteringMode = model.TokenPlanMeteringWeighted
	}
	return ""
}

func AdminCreateTokenPlan(c *gin.Context) {
	var req AdminUpsertTokenPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	req.Plan.Id = 0
	req.Plan.NormalizeDefaults()
	if msg := validateTokenPlan(&req.Plan); msg != "" {
		common.ApiErrorT(c, msg)
		return
	}
	if err := model.DB.Create(&req.Plan).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, req.Plan)
}

func AdminUpdateTokenPlan(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	existing, err := model.GetTokenPlanById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req AdminUpsertTokenPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	req.Plan.NormalizeDefaults()
	if msg := validateTokenPlan(&req.Plan); msg != "" {
		common.ApiErrorT(c, msg)
		return
	}
	// 显式 updateMap：GORM 的 struct 更新会丢弃零值字段（例如把 enabled 改回 false）。
	updates := map[string]any{
		"title":           req.Plan.Title,
		"subtitle":        req.Plan.Subtitle,
		"price_amount":    req.Plan.PriceAmount,
		"currency":        req.Plan.Currency,
		"duration_days":   req.Plan.DurationDays,
		"total_tokens":    req.Plan.TotalTokens,
		"window1_seconds": req.Plan.Window1Seconds,
		"window1_tokens":  req.Plan.Window1Tokens,
		"window2_seconds": req.Plan.Window2Seconds,
		"window2_tokens":  req.Plan.Window2Tokens,
		"metering_mode":   req.Plan.MeteringMode,
		"model_limits":    req.Plan.ModelLimits,
		"enabled":         req.Plan.Enabled,
		"sort_order":      req.Plan.SortOrder,
		"updated_at":      common.GetTimestamp(),
	}
	if err := model.DB.Model(&model.TokenPlan{}).Where("id = ?", existing.Id).Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	updated, err := model.GetTokenPlanById(existing.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, updated)
}

func AdminDeleteTokenPlan(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	if err := model.DB.Delete(&model.TokenPlan{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
