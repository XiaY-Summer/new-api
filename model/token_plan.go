package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TOKEN PLAN 订阅：套餐内发放 token 额度，并受两个锚定式滚动窗口约束。
// 与既有的 model/subscription.go 完全独立：后者以 quota（金额）计量、按日历周期重置，
// 本模块以 token 计量、按「首次使用时间 + 窗口时长」切分窗口。

const (
	TokenPlanStatusActive    = "active"
	TokenPlanStatusExpired   = "expired"
	TokenPlanStatusCancelled = "cancelled"

	TokenPlanMeteringWeighted = "weighted"
	TokenPlanMeteringRaw      = "raw"

	// TokenPlanKeyPrefix 是专用 key 的前缀。服务端在认证阶段需特殊处理，
	// 不能按 "-" 切分（见 middleware/auth.go）。
	TokenPlanKeyPrefix = "token-"

	DefaultTokenPlanWindow1Seconds int64 = 5 * 60 * 60
	DefaultTokenPlanWindow2Seconds int64 = 7 * 24 * 60 * 60
)

// TokenPlan 是管理员维护的套餐模板。
type TokenPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(64);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'CNY'"`

	// DurationDays 套餐有效期（天），默认 30。
	DurationDays int `json:"duration_days" gorm:"type:int;not null;default:30"`

	// TotalTokens 套餐总额度（标准 token 数，加权折算后累计）。
	TotalTokens int64 `json:"total_tokens" gorm:"type:bigint;not null;default:0"`

	// 窗口 1（默认 5 小时）
	Window1Seconds int64 `json:"window1_seconds" gorm:"type:bigint;not null;default:0"`
	Window1Tokens  int64 `json:"window1_tokens" gorm:"type:bigint;not null;default:0"`

	// 窗口 2（默认 7 天）
	Window2Seconds int64 `json:"window2_seconds" gorm:"type:bigint;not null;default:0"`
	Window2Tokens  int64 `json:"window2_tokens" gorm:"type:bigint;not null;default:0"`

	// MeteringMode: weighted（实际 token × 模型倍率）| raw（实际 token）
	MeteringMode string `json:"metering_mode" gorm:"type:varchar(16);not null;default:'weighted'"`

	// ModelLimits 可用模型，逗号分隔，空 = 不限。
	ModelLimits string `json:"model_limits" gorm:"type:text"`

	// Enabled 默认 false：GORM 创建时会忽略零值字段，只有列默认值同为 false 才能正确落库。
	Enabled   bool  `json:"enabled" gorm:"not null;default:false"`
	SortOrder int   `json:"sort_order" gorm:"type:int;not null;default:0"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *TokenPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *TokenPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *TokenPlan) NormalizeDefaults() {
	if p.Currency == "" {
		p.Currency = "CNY"
	}
	if p.MeteringMode != TokenPlanMeteringRaw {
		p.MeteringMode = TokenPlanMeteringWeighted
	}
	if p.DurationDays <= 0 {
		p.DurationDays = 30
	}
	if p.Window1Seconds <= 0 {
		p.Window1Seconds = DefaultTokenPlanWindow1Seconds
	}
	if p.Window2Seconds <= 0 {
		p.Window2Seconds = DefaultTokenPlanWindow2Seconds
	}
}

// ModelLimitList 把逗号分隔的模型限制拆成切片，空值返回 nil（表示不限）。
func (p *TokenPlan) ModelLimitList() []string {
	return SplitModelLimits(p.ModelLimits)
}

func SplitModelLimits(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// UserTokenPlan 是用户持有的套餐实例。下单时对套餐参数做快照，
// 管理员后续修改模板不会影响已购用户。
type UserTokenPlan struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index:idx_user_token_plan_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	PlanTitle string `json:"plan_title" gorm:"type:varchar(64);default:''"`

	TotalTokens    int64  `json:"total_tokens" gorm:"type:bigint;not null;default:0"`
	Window1Seconds int64  `json:"window1_seconds" gorm:"type:bigint;not null;default:0"`
	Window1Tokens  int64  `json:"window1_tokens" gorm:"type:bigint;not null;default:0"`
	Window2Seconds int64  `json:"window2_seconds" gorm:"type:bigint;not null;default:0"`
	Window2Tokens  int64  `json:"window2_tokens" gorm:"type:bigint;not null;default:0"`
	MeteringMode   string `json:"metering_mode" gorm:"type:varchar(16);default:'weighted'"`
	ModelLimits    string `json:"model_limits" gorm:"type:text"`

	// AnchorAt 首次使用时间，0 表示尚未开始计时。
	AnchorAt int64 `json:"anchor_at" gorm:"type:bigint;not null;default:0"`

	// 窗口运行态：Index 是当前窗口序号，Used 是该窗口内已消耗的加权 token。
	Window1Index int64 `json:"window1_index" gorm:"type:bigint;not null;default:0"`
	Window1Used  int64 `json:"window1_used" gorm:"type:bigint;not null;default:0"`
	Window2Index int64 `json:"window2_index" gorm:"type:bigint;not null;default:0"`
	Window2Used  int64 `json:"window2_used" gorm:"type:bigint;not null;default:0"`

	UsedTokens int64 `json:"used_tokens" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"type:bigint;not null;default:0"`
	EndTime   int64  `json:"end_time" gorm:"type:bigint;not null;default:0"`
	Status    string `json:"status" gorm:"type:varchar(32);index:idx_user_token_plan_active,priority:2"`

	// TokenId 指向专用 API key（tokens.id）。
	TokenId int `json:"token_id" gorm:"default:0"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (p *UserTokenPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedTime = now
	p.UpdatedTime = now
	return nil
}

func (p *UserTokenPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedTime = common.GetTimestamp()
	return nil
}

// WindowIndex 计算锚定式窗口序号：首次使用时间为锚点，每经过 seconds 秒进入下一个窗口。
func WindowIndex(anchorAt int64, now int64, seconds int64) int64 {
	if anchorAt <= 0 || seconds <= 0 || now < anchorAt {
		return 0
	}
	return (now - anchorAt) / seconds
}

// TokenPlanWindowView 是返回给前端的单个窗口视图，字段命名对齐 Codex 的用量结构。
type TokenPlanWindowView struct {
	Key               string `json:"key"` // window1 / window2
	Seconds           int64  `json:"limit_window_seconds"`
	LimitTokens       int64  `json:"limit_tokens"`
	UsedTokens        int64  `json:"used_tokens"`
	RemainingTokens   int64  `json:"remaining_tokens"`
	UsedPercent       int    `json:"used_percent"`
	ResetAt           int64  `json:"reset_at"`
	ResetAfterSeconds int64  `json:"reset_after_seconds"`
	Exhausted         bool   `json:"exhausted"`
}

// BuildWindowView 基于当前时间推导窗口视图。当存储的窗口序号与当前序号不一致时，
// 说明窗口已经翻篇，视为用量归零（无需写库即可得到正确读数）。
func BuildWindowView(key string, anchorAt int64, seconds int64, limit int64, storedIndex int64, storedUsed int64, now int64) TokenPlanWindowView {
	idx := WindowIndex(anchorAt, now, seconds)
	used := storedUsed
	if idx != storedIndex {
		used = 0
	}
	if used < 0 {
		used = 0
	}
	remaining := limit - used
	if limit <= 0 {
		remaining = 0
	}
	if remaining < 0 {
		remaining = 0
	}
	percent := 0
	if limit > 0 {
		percent = int(math.Round(float64(used) * 100 / float64(limit)))
		if percent > 100 {
			percent = 100
		}
	}
	view := TokenPlanWindowView{
		Key:             key,
		Seconds:         seconds,
		LimitTokens:     limit,
		UsedTokens:      used,
		RemainingTokens: remaining,
		UsedPercent:     percent,
		Exhausted:       limit > 0 && used >= limit,
	}
	if anchorAt > 0 && seconds > 0 {
		view.ResetAt = anchorAt + (idx+1)*seconds
		view.ResetAfterSeconds = view.ResetAt - now
		if view.ResetAfterSeconds < 0 {
			view.ResetAfterSeconds = 0
		}
	}
	return view
}

// WindowViews 返回两个窗口的当前视图。
func (p *UserTokenPlan) WindowViews(now int64) (TokenPlanWindowView, TokenPlanWindowView) {
	w1 := BuildWindowView("window1", p.AnchorAt, p.Window1Seconds, p.Window1Tokens, p.Window1Index, p.Window1Used, now)
	w2 := BuildWindowView("window2", p.AnchorAt, p.Window2Seconds, p.Window2Tokens, p.Window2Index, p.Window2Used, now)
	return w1, w2
}

// IsExpired 判断套餐是否已过期。
func (p *UserTokenPlan) IsExpired(now int64) bool {
	return p.Status != TokenPlanStatusActive || (p.EndTime > 0 && p.EndTime <= now)
}

// WeightedTokens 按计量模式折算本次请求消耗的 token。
func WeightedTokens(tokens int, modelRatio float64, meteringMode string) int64 {
	if tokens <= 0 {
		return 0
	}
	if meteringMode == TokenPlanMeteringRaw || modelRatio <= 0 {
		return int64(tokens)
	}
	return int64(math.Round(float64(tokens) * modelRatio))
}

// ---- 查询 ----

func GetEnabledTokenPlans() ([]*TokenPlan, error) {
	var plans []*TokenPlan
	err := DB.Where("enabled = ?", true).Order("sort_order desc, id asc").Find(&plans).Error
	return plans, err
}

func GetAllTokenPlans() ([]*TokenPlan, error) {
	var plans []*TokenPlan
	err := DB.Order("sort_order desc, id asc").Find(&plans).Error
	return plans, err
}

func GetTokenPlanById(id int) (*TokenPlan, error) {
	var plan TokenPlan
	if err := DB.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

// GetActiveUserTokenPlan 返回用户当前生效的套餐，没有则返回 (nil, nil)。
func GetActiveUserTokenPlan(userId int) (*UserTokenPlan, error) {
	now := common.GetTimestamp()
	var plan UserTokenPlan
	err := DB.Where("user_id = ? AND status = ? AND end_time > ?", userId, TokenPlanStatusActive, now).
		Order("id desc").
		First(&plan).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// GetUserTokenPlanById 按主键读取套餐实例（供中间件按 context 中的 plan id 查询）。
func GetUserTokenPlanById(id int) (*UserTokenPlan, error) {
	if id <= 0 {
		return nil, nil
	}
	var plan UserTokenPlan
	err := DB.Where("id = ?", id).First(&plan).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// ---- 计量 ----

// RecordUsage 累加一次请求的加权 token 消耗。
// 单事务内完成：锁行 → 窗口翻篇则清零 → 累加 → 首次写入锚点。
// 调用方（结算流程）应容忍错误：计量失败不应影响已经返回给用户的结果。
func RecordUsage(userId int, planId int, tokens int, modelRatio float64, meteringMode string) error {
	delta := WeightedTokens(tokens, modelRatio, meteringMode)
	if delta <= 0 || planId <= 0 {
		return nil
	}
	now := common.GetTimestamp()

	return DB.Transaction(func(tx *gorm.DB) error {
		var plan UserTokenPlan
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", planId, userId).First(&plan).Error; err != nil {
			return err
		}

		updates := map[string]any{}
		anchorAt := plan.AnchorAt
		if anchorAt <= 0 {
			anchorAt = now
			updates["anchor_at"] = anchorAt
		}

		idx1 := WindowIndex(anchorAt, now, plan.Window1Seconds)
		if idx1 != plan.Window1Index {
			updates["window1_index"] = idx1
			updates["window1_used"] = delta
		} else {
			updates["window1_used"] = gorm.Expr("window1_used + ?", delta)
		}

		idx2 := WindowIndex(anchorAt, now, plan.Window2Seconds)
		if idx2 != plan.Window2Index {
			updates["window2_index"] = idx2
			updates["window2_used"] = delta
		} else {
			updates["window2_used"] = gorm.Expr("window2_used + ?", delta)
		}

		updates["used_tokens"] = gorm.Expr("used_tokens + ?", delta)
		updates["updated_time"] = now

		return tx.Model(&UserTokenPlan{}).Where("id = ?", plan.Id).Updates(updates).Error
	})
}

// ---- 购买与 key 管理 ----

// GenerateTokenPlanKey 生成 token- 前缀的专用 key（含前缀，直接存库）。
func GenerateTokenPlanKey() (string, error) {
	raw, err := common.GenerateRandomCharsKey(48)
	if err != nil {
		return "", err
	}
	return TokenPlanKeyPrefix + raw, nil
}

// PurchaseTokenPlan 是占位版的购买流程：不接支付，直接为当前用户开通套餐。
// 已有生效套餐时旧记录置为 cancelled 并复用原专用 key（key id 不变，仅重写 key 串）。
func PurchaseTokenPlan(userId int, planId int) (*UserTokenPlan, *Token, error) {
	plan, err := GetTokenPlanById(planId)
	if err != nil {
		return nil, nil, errors.New("套餐不存在")
	}
	if !plan.Enabled {
		return nil, nil, errors.New("套餐已下架")
	}
	plan.NormalizeDefaults()

	now := common.GetTimestamp()
	var result *UserTokenPlan
	var resultToken *Token

	err = DB.Transaction(func(tx *gorm.DB) error {
		// 作废旧套餐
		if err := tx.Model(&UserTokenPlan{}).
			Where("user_id = ? AND status = ?", userId, TokenPlanStatusActive).
			Updates(map[string]any{"status": TokenPlanStatusCancelled, "updated_time": now}).Error; err != nil {
			return err
		}

		sub := &UserTokenPlan{
			UserId:         userId,
			PlanId:         plan.Id,
			PlanTitle:      plan.Title,
			TotalTokens:    plan.TotalTokens,
			Window1Seconds: plan.Window1Seconds,
			Window1Tokens:  plan.Window1Tokens,
			Window2Seconds: plan.Window2Seconds,
			Window2Tokens:  plan.Window2Tokens,
			MeteringMode:   plan.MeteringMode,
			ModelLimits:    plan.ModelLimits,
			StartTime:      now,
			EndTime:        now + int64(plan.DurationDays)*86400,
			Status:         TokenPlanStatusActive,
		}
		if err := tx.Create(sub).Error; err != nil {
			return err
		}

		// 复用该用户已有的专用 key，没有则新建
		var token Token
		tokenErr := tx.Where("user_id = ? AND token_plan_id IS NOT NULL", userId).Order("id asc").First(&token).Error
		if tokenErr != nil && !errors.Is(tokenErr, gorm.ErrRecordNotFound) {
			return tokenErr
		}
		if errors.Is(tokenErr, gorm.ErrRecordNotFound) {
			newKey, keyErr := GenerateTokenPlanKey()
			if keyErr != nil {
				return keyErr
			}
			token = Token{
				UserId:         userId,
				Key:            newKey,
				Status:         common.TokenStatusEnabled,
				Name:           "TOKEN PLAN 专用密钥",
				CreatedTime:    now,
				AccessedTime:   now,
				ExpiredTime:    -1,
				RemainQuota:    0,
				UnlimitedQuota: true,
				Group:          "",
			}
			if err := tx.Create(&token).Error; err != nil {
				return err
			}
		}
		planIdCopy := sub.Id
		if token.Key != "" {
			_ = invalidateTokenCacheForMutation(token.Key)
		}
		if err := tx.Model(&Token{}).Where("id = ?", token.Id).
			Updates(map[string]any{"token_plan_id": planIdCopy, "unlimited_quota": true, "status": common.TokenStatusEnabled}).Error; err != nil {
			return err
		}
		token.TokenPlanId = &planIdCopy

		if err := tx.Model(&UserTokenPlan{}).Where("id = ?", sub.Id).
			Update("token_id", token.Id).Error; err != nil {
			return err
		}
		sub.TokenId = token.Id

		result = sub
		resultToken = &token
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return result, resultToken, nil
}

// ResetTokenPlanKey 为当前用户重新生成专用 key（原地更新，保持 key id 与套餐绑定不变）。
func ResetTokenPlanKey(userId int) (*Token, error) {
	sub, err := GetActiveUserTokenPlan(userId)
	if err != nil {
		return nil, err
	}
	if sub == nil || sub.TokenId <= 0 {
		return nil, errors.New("当前没有生效中的 TOKEN PLAN")
	}
	// 旧 key 的缓存必须在写库前失效，否则它在 TTL 内仍能通过认证。
	var old Token
	if err := DB.Select("id", "key").Where("id = ?", sub.TokenId).First(&old).Error; err == nil {
		_ = invalidateTokenCacheForMutation(old.Key)
	}
	newKey, err := GenerateTokenPlanKey()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	if err := DB.Model(&Token{}).Where("id = ? AND user_id = ?", sub.TokenId, userId).
		Updates(map[string]any{"key": newKey, "accessed_time": now}).Error; err != nil {
		return nil, err
	}
	token, err := GetTokenByIds(sub.TokenId, userId)
	if err != nil {
		return nil, err
	}
	return token, nil
}

// ---- 种子数据 ----

// EnsureTokenPlanSeed 在套餐表为空时写入三个占位套餐，方便开箱验证。
func EnsureTokenPlanSeed() error {
	var count int64
	if err := DB.Model(&TokenPlan{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	presets := []TokenPlan{
		{
			Title: "PLUS", Subtitle: "轻量体验，适合日常对话与轻代码",
			PriceAmount: 59, Currency: "CNY", DurationDays: 30,
			TotalTokens:    5_000_000,
			Window1Seconds: DefaultTokenPlanWindow1Seconds, Window1Tokens: 1_000_000,
			Window2Seconds: DefaultTokenPlanWindow2Seconds, Window2Tokens: 3_000_000,
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true, SortOrder: 30,
		},
		{
			Title: "PRO", Subtitle: "主力档位，覆盖大部分开发场景",
			PriceAmount: 159, Currency: "CNY", DurationDays: 30,
			TotalTokens:    15_000_000,
			Window1Seconds: DefaultTokenPlanWindow1Seconds, Window1Tokens: 3_000_000,
			Window2Seconds: DefaultTokenPlanWindow2Seconds, Window2Tokens: 9_000_000,
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true, SortOrder: 20,
		},
		{
			Title: "MAX", Subtitle: "高强度使用，长上下文与批量任务",
			PriceAmount: 359, Currency: "CNY", DurationDays: 30,
			TotalTokens:    40_000_000,
			Window1Seconds: DefaultTokenPlanWindow1Seconds, Window1Tokens: 8_000_000,
			Window2Seconds: DefaultTokenPlanWindow2Seconds, Window2Tokens: 25_000_000,
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true, SortOrder: 10,
		},
	}
	for i := range presets {
		if err := DB.Create(&presets[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
