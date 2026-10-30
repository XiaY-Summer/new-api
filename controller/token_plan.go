package controller

import (
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// ---- 套餐 DTO ----
//
// 窗口时长在数据库里以秒存储（内部窗口序号推导按秒计算），对外统一用分钟：
// 管理员填 18000 这种数字既容易算错，也看不出是多久。

type TokenPlanView struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`

	PriceAmount float64 `json:"price_amount"`
	Currency    string  `json:"currency"`

	DurationDays int   `json:"duration_days"`
	TotalTokens  int64 `json:"total_tokens"`

	Window1Minutes int   `json:"window1_minutes"`
	Window1Tokens  int64 `json:"window1_tokens"`
	Window2Minutes int   `json:"window2_minutes"`
	Window2Tokens  int64 `json:"window2_tokens"`

	MeteringMode string `json:"metering_mode"`
	ModelLimits  string `json:"model_limits"`
	GroupLimits  string `json:"group_limits"`

	Enabled   bool  `json:"enabled"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

func newTokenPlanView(p *model.TokenPlan) TokenPlanView {
	return TokenPlanView{
		Id:             p.Id,
		Title:          p.Title,
		Subtitle:       p.Subtitle,
		PriceAmount:    p.PriceAmount,
		Currency:       p.Currency,
		DurationDays:   p.DurationDays,
		TotalTokens:    p.TotalTokens,
		Window1Minutes: int(p.Window1Seconds / 60),
		Window1Tokens:  p.Window1Tokens,
		Window2Minutes: int(p.Window2Seconds / 60),
		Window2Tokens:  p.Window2Tokens,
		MeteringMode:   p.MeteringMode,
		ModelLimits:    p.ModelLimits,
		GroupLimits:    p.GroupLimits,
		Enabled:        p.Enabled,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

func newTokenPlanViews(plans []*model.TokenPlan) []TokenPlanView {
	views := make([]TokenPlanView, 0, len(plans))
	for _, p := range plans {
		p.NormalizeDefaults()
		views = append(views, newTokenPlanView(p))
	}
	return views
}

// toModel 把 DTO 还原成数据模型。Id 由调用方决定，分钟转秒在此完成。
func (v TokenPlanView) toModel() model.TokenPlan {
	return model.TokenPlan{
		Id:             v.Id,
		Title:          v.Title,
		Subtitle:       v.Subtitle,
		PriceAmount:    v.PriceAmount,
		Currency:       v.Currency,
		DurationDays:   v.DurationDays,
		TotalTokens:    v.TotalTokens,
		Window1Seconds: int64(v.Window1Minutes) * 60,
		Window1Tokens:  v.Window1Tokens,
		Window2Seconds: int64(v.Window2Minutes) * 60,
		Window2Tokens:  v.Window2Tokens,
		MeteringMode:   v.MeteringMode,
		ModelLimits:    v.ModelLimits,
		GroupLimits:    v.GroupLimits,
		Enabled:        v.Enabled,
	}
}

// ---- 用户侧 ----

// GetTokenPlanPlans 返回上架中的 TOKEN PLAN 套餐列表。
func GetTokenPlanPlans(c *gin.Context) {
	plans, err := model.GetEnabledTokenPlans()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, newTokenPlanViews(plans))
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
	// 空切片而非 nil，避免前端拿到 null 还要额外判空。
	groups := model.SplitModelLimits(sub.GroupLimits)
	if groups == nil {
		groups = []string{}
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
		"groups":       groups,
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
	common.ApiSuccess(c, newTokenPlanViews(plans))
}

type AdminUpsertTokenPlanRequest struct {
	Plan TokenPlanView `json:"plan"`
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
	plan := req.Plan.toModel()
	plan.Id = 0
	plan.NormalizeDefaults()
	if msg := validateTokenPlan(&plan); msg != "" {
		common.ApiErrorT(c, msg)
		return
	}
	if err := model.DB.Create(&plan).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, newTokenPlanView(&plan))
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
	plan := req.Plan.toModel()
	plan.NormalizeDefaults()
	if msg := validateTokenPlan(&plan); msg != "" {
		common.ApiErrorT(c, msg)
		return
	}
	// 显式 updateMap：GORM 的 struct 更新会丢弃零值字段（例如把 enabled 改回 false）。
	updates := map[string]any{
		"title":           plan.Title,
		"subtitle":        plan.Subtitle,
		"price_amount":    plan.PriceAmount,
		"currency":        plan.Currency,
		"duration_days":   plan.DurationDays,
		"total_tokens":    plan.TotalTokens,
		"window1_seconds": plan.Window1Seconds,
		"window1_tokens":  plan.Window1Tokens,
		"window2_seconds": plan.Window2Seconds,
		"window2_tokens":  plan.Window2Tokens,
		"metering_mode":   plan.MeteringMode,
		"model_limits":    plan.ModelLimits,
		"group_limits":    plan.GroupLimits,
		"enabled":         plan.Enabled,
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
	common.ApiSuccess(c, newTokenPlanView(updated))
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

// AdminGetTokenPlanGroupModels 返回「分组 -> 可用模型」的映射，供套餐表单按分组带入模型。
// 一次返回全部分组，前端切换分组时不必再请求。
func AdminGetTokenPlanGroupModels(c *gin.Context) {
	groups := ratio_setting.GetGroupRatioCopy()
	result := make(map[string][]string, len(groups))
	for name := range groups {
		models := model.GetGroupEnabledModels(name)
		sort.Strings(models)
		if models == nil {
			models = []string{}
		}
		result[name] = models
	}
	common.ApiSuccess(c, result)
}

// AdminListTokenPlanSubscribers 返回生效中的订阅列表，供管理端查看与重置用量。
func AdminListTokenPlanSubscribers(c *gin.Context) {
	subs, err := model.GetTokenPlanSubscribers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, subs)
}

type AdminResetTokenPlanUsageRequest struct {
	// UserId 为 0 或缺省表示重置全体用户。
	UserId int `json:"user_id"`
}

// AdminResetTokenPlanUsage 清零套餐用量并重置窗口锚点。
func AdminResetTokenPlanUsage(c *gin.Context) {
	var req AdminResetTokenPlanUsageRequest
	_ = c.ShouldBindJSON(&req)
	if req.UserId < 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	affected, err := model.ResetTokenPlanUsage(req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"affected": affected})
}
