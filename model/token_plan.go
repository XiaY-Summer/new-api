package model

import (
	"errors"
	"math"
	"strings"
	"time"

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

	// GroupLimits 可用分组，逗号分隔，空 = 不限。
	// 请求时校验 token 实际使用的分组是否在此列表内。
	GroupLimits string `json:"group_limits" gorm:"type:text"`

	// 节流配置，见 TokenPlanThrottle。布尔字段必须带 default:false，
	// 否则 GORM 创建时跳过零值会导致列默认值不同步。
	FirstByteDelayEnabled         bool  `json:"first_byte_delay_enabled" gorm:"not null;default:false"`
	FirstByteDelayPromptThreshold int   `json:"first_byte_delay_prompt_threshold" gorm:"type:int;not null;default:0"`
	FirstByteDelayProbability     int   `json:"first_byte_delay_probability" gorm:"type:int;not null;default:0"`
	FirstByteDelayMinMs           int   `json:"first_byte_delay_min_ms" gorm:"type:int;not null;default:0"`
	FirstByteDelayMaxMs           int   `json:"first_byte_delay_max_ms" gorm:"type:int;not null;default:0"`
	ConcurrencyLimit              int   `json:"concurrency_limit" gorm:"type:int;not null;default:0"`
	RateLimitCount                int   `json:"rate_limit_count" gorm:"type:int;not null;default:0"`
	RateLimitWindowSeconds        int64 `json:"rate_limit_window_seconds" gorm:"type:bigint;not null;default:0"`

	// Enabled 默认 false：GORM 创建时会忽略零值字段，只有列默认值同为 false 才能正确落库。
	Enabled   bool  `json:"enabled" gorm:"not null;default:false"`
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
	t := p.Throttle().Normalize()
	p.FirstByteDelayEnabled = t.FirstByteDelayEnabled
	p.FirstByteDelayPromptThreshold = t.FirstByteDelayPromptThreshold
	p.FirstByteDelayProbability = t.FirstByteDelayProbability
	p.FirstByteDelayMinMs = t.FirstByteDelayMinMs
	p.FirstByteDelayMaxMs = t.FirstByteDelayMaxMs
	p.ConcurrencyLimit = t.ConcurrencyLimit
	p.RateLimitCount = t.RateLimitCount
	p.RateLimitWindowSeconds = t.RateLimitWindowSeconds
}

// ModelLimitList 把逗号分隔的模型限制拆成切片，空值返回 nil（表示不限）。
func (p *TokenPlan) ModelLimitList() []string {
	return SplitModelLimits(p.ModelLimits)
}

// GroupLimitList 把逗号分隔的分组限制拆成切片，空值返回 nil（表示不限）。
func (p *TokenPlan) GroupLimitList() []string {
	return SplitModelLimits(p.GroupLimits)
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

// TokenPlanThrottle 是套餐的节流参数：首字延迟、并发上限、频率上限。
// 单独抽出来是为了让判定逻辑成为纯函数，便于单测，也便于在中间件与 relay 之间传递。
type TokenPlanThrottle struct {
	FirstByteDelayEnabled         bool
	FirstByteDelayPromptThreshold int
	FirstByteDelayProbability     int
	FirstByteDelayMinMs           int
	FirstByteDelayMaxMs           int
	ConcurrencyLimit              int
	RateLimitCount                int
	RateLimitWindowSeconds        int64
}

// Throttle 从套餐模板提取节流参数。
func (p *TokenPlan) Throttle() TokenPlanThrottle {
	if p == nil {
		return TokenPlanThrottle{}
	}
	return TokenPlanThrottle{
		FirstByteDelayEnabled:         p.FirstByteDelayEnabled,
		FirstByteDelayPromptThreshold: p.FirstByteDelayPromptThreshold,
		FirstByteDelayProbability:     p.FirstByteDelayProbability,
		FirstByteDelayMinMs:           p.FirstByteDelayMinMs,
		FirstByteDelayMaxMs:           p.FirstByteDelayMaxMs,
		ConcurrencyLimit:              p.ConcurrencyLimit,
		RateLimitCount:                p.RateLimitCount,
		RateLimitWindowSeconds:        p.RateLimitWindowSeconds,
	}
}

// throttleUpdates 返回节流列的更新映射。GORM 的 struct 更新会丢弃零值字段，
// 因此「续费刷新」与「同步到订阅」都必须走显式 map，否则把开关关掉、把上限改回 0 会静默失效。
func (p *TokenPlan) throttleUpdates() map[string]any {
	return map[string]any{
		"first_byte_delay_enabled":          p.FirstByteDelayEnabled,
		"first_byte_delay_prompt_threshold": p.FirstByteDelayPromptThreshold,
		"first_byte_delay_probability":      p.FirstByteDelayProbability,
		"first_byte_delay_min_ms":           p.FirstByteDelayMinMs,
		"first_byte_delay_max_ms":           p.FirstByteDelayMaxMs,
		"concurrency_limit":                 p.ConcurrencyLimit,
		"rate_limit_count":                  p.RateLimitCount,
		"rate_limit_window_seconds":         p.RateLimitWindowSeconds,
	}
}

// Throttle 从用户套餐快照提取节流参数。
func (p *UserTokenPlan) Throttle() TokenPlanThrottle {
	if p == nil {
		return TokenPlanThrottle{}
	}
	return TokenPlanThrottle{
		FirstByteDelayEnabled:         p.FirstByteDelayEnabled,
		FirstByteDelayPromptThreshold: p.FirstByteDelayPromptThreshold,
		FirstByteDelayProbability:     p.FirstByteDelayProbability,
		FirstByteDelayMinMs:           p.FirstByteDelayMinMs,
		FirstByteDelayMaxMs:           p.FirstByteDelayMaxMs,
		ConcurrencyLimit:              p.ConcurrencyLimit,
		RateLimitCount:                p.RateLimitCount,
		RateLimitWindowSeconds:        p.RateLimitWindowSeconds,
	}
}

// applyThrottle 把模板的节流参数写进快照。新建记录与续费刷新共用，避免两处漏字段。
func (p *UserTokenPlan) applyThrottle(plan *TokenPlan) {
	t := plan.Throttle()
	p.FirstByteDelayEnabled = t.FirstByteDelayEnabled
	p.FirstByteDelayPromptThreshold = t.FirstByteDelayPromptThreshold
	p.FirstByteDelayProbability = t.FirstByteDelayProbability
	p.FirstByteDelayMinMs = t.FirstByteDelayMinMs
	p.FirstByteDelayMaxMs = t.FirstByteDelayMaxMs
	p.ConcurrencyLimit = t.ConcurrencyLimit
	p.RateLimitCount = t.RateLimitCount
	p.RateLimitWindowSeconds = t.RateLimitWindowSeconds
}

// Normalize 收敛非法输入，保证后续判定不会因为脏数据产生怪异行为。
func (t TokenPlanThrottle) Normalize() TokenPlanThrottle {
	if t.FirstByteDelayPromptThreshold < 0 {
		t.FirstByteDelayPromptThreshold = 0
	}
	if t.FirstByteDelayProbability < 0 {
		t.FirstByteDelayProbability = 0
	}
	if t.FirstByteDelayProbability > 100 {
		t.FirstByteDelayProbability = 100
	}
	if t.FirstByteDelayMinMs < 0 {
		t.FirstByteDelayMinMs = 0
	}
	if t.FirstByteDelayMaxMs < 0 {
		t.FirstByteDelayMaxMs = 0
	}
	if t.FirstByteDelayMaxMs < t.FirstByteDelayMinMs {
		t.FirstByteDelayMaxMs = t.FirstByteDelayMinMs
	}
	if t.ConcurrencyLimit < 0 {
		t.ConcurrencyLimit = 0
	}
	if t.RateLimitCount < 0 {
		t.RateLimitCount = 0
	}
	if t.RateLimitCount > 0 && t.RateLimitWindowSeconds <= 0 {
		t.RateLimitWindowSeconds = 60
	}
	if t.RateLimitWindowSeconds < 0 {
		t.RateLimitWindowSeconds = 0
	}
	return t
}

// DecideFirstByteDelay 判定本次请求是否要延迟首字，以及延迟多久。
//
// 顺序：开关 -> 上下文阈值 -> 概率 -> 时长随机。roll 注入是为了让单测完全确定，
// 生产传 rand.Float64 即可（全局无锁，不需要 seed）。
func (t TokenPlanThrottle) DecideFirstByteDelay(promptTokens int, roll func() float64) time.Duration {
	if !t.FirstByteDelayEnabled {
		return 0
	}
	if t.FirstByteDelayPromptThreshold > 0 && promptTokens < t.FirstByteDelayPromptThreshold {
		return 0
	}
	if t.FirstByteDelayProbability <= 0 || t.FirstByteDelayMaxMs <= 0 {
		return 0
	}
	if t.FirstByteDelayProbability < 100 && roll()*100 >= float64(t.FirstByteDelayProbability) {
		return 0
	}
	minMs, maxMs := t.FirstByteDelayMinMs, t.FirstByteDelayMaxMs
	if minMs < 0 {
		minMs = 0
	}
	if maxMs < minMs {
		maxMs = minMs
	}
	ms := minMs
	if span := maxMs - minMs; span > 0 {
		ms += int(roll() * float64(span+1))
		if ms > maxMs {
			ms = maxMs
		}
	}
	return time.Duration(ms) * time.Millisecond
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
	GroupLimits    string `json:"group_limits" gorm:"type:text"`

	// 节流配置的快照，含义同 TokenPlan 上同名字段。
	FirstByteDelayEnabled         bool  `json:"first_byte_delay_enabled" gorm:"not null;default:false"`
	FirstByteDelayPromptThreshold int   `json:"first_byte_delay_prompt_threshold" gorm:"type:int;not null;default:0"`
	FirstByteDelayProbability     int   `json:"first_byte_delay_probability" gorm:"type:int;not null;default:0"`
	FirstByteDelayMinMs           int   `json:"first_byte_delay_min_ms" gorm:"type:int;not null;default:0"`
	FirstByteDelayMaxMs           int   `json:"first_byte_delay_max_ms" gorm:"type:int;not null;default:0"`
	ConcurrencyLimit              int   `json:"concurrency_limit" gorm:"type:int;not null;default:0"`
	RateLimitCount                int   `json:"rate_limit_count" gorm:"type:int;not null;default:0"`
	RateLimitWindowSeconds        int64 `json:"rate_limit_window_seconds" gorm:"type:bigint;not null;default:0"`

	// AnchorAt 首次使用时间，0 表示尚未开始计时。
	AnchorAt int64 `json:"anchor_at" gorm:"type:bigint;not null;default:0"`

	// 窗口运行态：Index 是当前窗口序号，Used 是该窗口内已消耗的加权 token。
	Window1Index int64 `json:"window1_index" gorm:"type:bigint;not null;default:0"`
	Window1Used  int64 `json:"window1_used" gorm:"type:bigint;not null;default:0"`
	Window2Index int64 `json:"window2_index" gorm:"type:bigint;not null;default:0"`
	Window2Used  int64 `json:"window2_used" gorm:"type:bigint;not null;default:0"`

	UsedTokens   int64 `json:"used_tokens" gorm:"type:bigint;not null;default:0"`
	UsageVersion int64 `json:"-" gorm:"type:bigint;not null;default:0"`

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

// TokenPlanUsageDetail 描述一次请求的 token 构成，供加权折算使用。
// 输入 token 拆成「未命中缓存 / 命中缓存 / 写入缓存」三段，各自套用对应倍率：
// 缓存命中通常远低于原价（OpenAI 0.5x、Claude 0.1x），缓存写入则略高于原价
// （Claude 1.25x）。倍率缺失时按 1 处理，此时结果与
// 「(prompt + completion) × model_ratio」完全等价。
type TokenPlanUsageDetail struct {
	PromptTokens        int     // 未命中缓存的输入 token
	CacheTokens         int     // 命中缓存的输入 token
	CacheCreationTokens int     // 写入缓存的输入 token
	CompletionTokens    int     // 输出 token
	ModelRatio          float64 // 模型倍率
	CacheRatio          float64 // 缓存命中倍率
	CacheCreationRatio  float64 // 缓存写入倍率
}

// TotalTokens 返回本次请求的原始 token 总量（不做加权）。
func (d TokenPlanUsageDetail) TotalTokens() int {
	return common.QuotaRound(float64(max(d.PromptTokens, 0)) + float64(max(d.CacheTokens, 0)) +
		float64(max(d.CacheCreationTokens, 0)) + float64(max(d.CompletionTokens, 0)))
}

// normalizeRatio 把非法或未配置的倍率归一为 1（即不打折）。
func normalizeRatio(ratio float64) float64 {
	if ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 1
	}
	return ratio
}

// WeightedTokens 按计量模式折算本次请求消耗的 token。
// raw 模式或模型倍率缺失时退化为原始计数，此时仍会把缓存 token 计入总量。
func WeightedTokens(detail TokenPlanUsageDetail, meteringMode string) int64 {
	tokens, _ := WeightedTokensChecked(detail, meteringMode)
	return tokens
}

// WeightedTokensChecked keeps a single request within the common billing bound
// and exposes saturation to the request's consume log.
func WeightedTokensChecked(detail TokenPlanUsageDetail, meteringMode string) (int64, *common.QuotaClamp) {
	weighted := float64(max(detail.PromptTokens, 0)) + float64(max(detail.CompletionTokens, 0))
	cache, creation := 1.0, 1.0
	if meteringMode != TokenPlanMeteringRaw && detail.ModelRatio > 0 {
		cache, creation = normalizeRatio(detail.CacheRatio), normalizeRatio(detail.CacheCreationRatio)
	}
	weighted += float64(max(detail.CacheTokens, 0)) * cache
	weighted += float64(max(detail.CacheCreationTokens, 0)) * creation
	if meteringMode != TokenPlanMeteringRaw {
		weighted *= normalizeRatio(detail.ModelRatio)
	}
	tokens, clamp := common.QuotaRoundChecked(weighted)
	if weighted > 0 && tokens == 0 {
		tokens = 1
	}
	return int64(tokens), clamp
}

// ---- 查询 ----

// GetEnabledTokenPlans 返回上架中的套餐，按价格升序——低档位在前，
// 与用户端「从便宜到贵」的浏览顺序一致。原先靠 sort_order 手工排序，
// 只有三个套餐时这个字段既容易填错又看不出效果，已移除。
func GetEnabledTokenPlans() ([]*TokenPlan, error) {
	var plans []*TokenPlan
	err := DB.Where("enabled = ?", true).Order("price_amount asc, id asc").Find(&plans).Error
	return plans, err
}

func GetAllTokenPlans() ([]*TokenPlan, error) {
	var plans []*TokenPlan
	err := DB.Order("price_amount asc, id asc").Find(&plans).Error
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
func RecordUsage(userId int, planId int, detail TokenPlanUsageDetail, meteringMode string) error {
	delta := WeightedTokens(detail, meteringMode)
	if delta <= 0 || planId <= 0 {
		return nil
	}
	now := common.GetTimestamp()

	return DB.Transaction(func(tx *gorm.DB) error {
		plan, err := lockUserTokenPlan(tx, userId, planId)
		if err != nil {
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
//
// 三种情形：
//   - 同套餐续费：在原记录上累加有效期与总额度，已用用量保留；
//   - 升级（目标额度更高）：作废旧套餐，新建记录；
//   - 降级（目标额度更低）：拒绝，避免高挡位被一次误点覆盖。
//
// 无论哪种情形都复用该用户已有的专用 key（key id 不变）。
func PurchaseTokenPlan(userId int, planId int) (*UserTokenPlan, *Token, error) {
	plan, err := GetTokenPlanById(planId)
	if err != nil {
		return nil, nil, errors.New("套餐不存在")
	}
	if !plan.Enabled {
		return nil, nil, errors.New("套餐已下架")
	}
	plan.NormalizeDefaults()
	if plan.TotalTokens < 0 || plan.TotalTokens > common.MaxWalletQuota || plan.DurationDays > 36500 {
		return nil, nil, errors.New("Invalid parameters")
	}

	now := common.GetTimestamp()
	extendSeconds := int64(plan.DurationDays) * 86400

	var result *UserTokenPlan
	var resultToken *Token

	err = DB.Transaction(func(tx *gorm.DB) error {
		// Lock a stable row even on the first purchase, when no subscription
		// exists yet. SQLite acquires its write lock before the first read.
		if err := tx.Model(&User{}).Where("id = ?", userId).
			UpdateColumn("quota", gorm.Expr("quota")).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ? AND enabled = ?", plan.Id, true).First(plan).Error; err != nil {
			return err
		}
		plan.NormalizeDefaults()
		extendSeconds = int64(plan.DurationDays) * 86400
		var current UserTokenPlan
		currentErr := lockForUpdate(tx).
			Where("user_id = ? AND status = ?", userId, TokenPlanStatusActive).
			Order("id desc").
			First(&current).Error
		if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
			return currentErr
		}
		hasCurrent := currentErr == nil
		stillValid := hasCurrent && current.EndTime > now

		// 同套餐续费：累加时长与总额度，而不是把到期时间重置成「现在 + 一个月」。
		if hasCurrent && current.PlanId == plan.Id {
			if current.TotalTokens > common.MaxWalletQuota-plan.TotalTokens {
				return errors.New("Invalid parameters")
			}
			newEnd := current.EndTime + extendSeconds
			// 已过期的套餐从当前时间重新起算，否则会累加出「续了费还是过期」的结果。
			if !stillValid {
				newEnd = now + extendSeconds
			}
			updates := map[string]any{
				"total_tokens": gorm.Expr("total_tokens + ?", plan.TotalTokens),
				"end_time":     newEnd,
				"status":       TokenPlanStatusActive,
				"updated_time": now,
			}
			// 续费视为按当前条款重新购买：节流参数一并刷新到最新模板值，
			// 否则管理员收紧限制后，老用户靠续费就能永久沿用旧参数。
			for key, value := range plan.throttleUpdates() {
				updates[key] = value
			}
			if err := tx.Model(&UserTokenPlan{}).Where("id = ?", current.Id).Updates(updates).Error; err != nil {
				return err
			}
			current.TotalTokens += plan.TotalTokens
			current.EndTime = newEnd
			current.Status = TokenPlanStatusActive
			current.applyThrottle(plan)

			token, tokenErr := bindTokenPlanKey(tx, userId, current.Id, now)
			if tokenErr != nil {
				return tokenErr
			}
			current.TokenId = token.Id

			result = &current
			resultToken = token
			return nil
		}

		// 降级拦截：当前套餐仍在有效期内且额度更高时不允许被覆盖。
		if stillValid {
			var currentTemplate TokenPlan
			if err := tx.Where("id = ?", current.PlanId).First(&currentTemplate).Error; err != nil {
				return err
			}
			if currentTemplate.TotalTokens > plan.TotalTokens {
				return errors.New("当前套餐额度高于目标套餐，暂不支持降级；如需降级请先联系管理员")
			}
		}

		// 作废旧套餐
		if hasCurrent {
			if err := tx.Model(&UserTokenPlan{}).
				Where("id = ?", current.Id).
				Updates(map[string]any{"status": TokenPlanStatusCancelled, "updated_time": now}).Error; err != nil {
				return err
			}
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
			GroupLimits:    plan.GroupLimits,
			StartTime:      now,
			EndTime:        now + extendSeconds,
			Status:         TokenPlanStatusActive,
		}
		sub.applyThrottle(plan)
		if err := tx.Create(sub).Error; err != nil {
			return err
		}

		token, tokenErr := bindTokenPlanKey(tx, userId, sub.Id, now)
		if tokenErr != nil {
			return tokenErr
		}
		sub.TokenId = token.Id

		result = sub
		resultToken = token
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return result, resultToken, nil
}

// bindTokenPlanKey 复用该用户已有的专用 key（没有则新建），并把它绑定到指定套餐记录。
func bindTokenPlanKey(tx *gorm.DB, userId int, userPlanId int, now int64) (*Token, error) {
	var token Token
	tokenErr := tx.Unscoped().Where("user_id = ? AND token_plan_id IS NOT NULL", userId).Order("id asc").First(&token).Error
	if tokenErr != nil && !errors.Is(tokenErr, gorm.ErrRecordNotFound) {
		return nil, tokenErr
	}
	if errors.Is(tokenErr, gorm.ErrRecordNotFound) {
		newKey, keyErr := GenerateTokenPlanKey()
		if keyErr != nil {
			return nil, keyErr
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
			return nil, err
		}
	}
	// 旧 key 的缓存必须在写库前失效，否则它在 TTL 内仍会按旧的 token_plan_id 放行。
	if token.Key != "" {
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			return nil, err
		}
	}
	planIdCopy := userPlanId
	updates := map[string]any{"token_plan_id": planIdCopy, "unlimited_quota": true, "status": common.TokenStatusEnabled}
	if token.DeletedAt.Valid {
		newKey, err := GenerateTokenPlanKey()
		if err != nil {
			return nil, err
		}
		updates["key"], updates["deleted_at"] = newKey, nil
		token.Key = newKey
		token.DeletedAt = gorm.DeletedAt{}
	}
	if err := tx.Unscoped().Model(&Token{}).Where("id = ?", token.Id).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&UserTokenPlan{}).Where("id = ?", userPlanId).
		Update("token_id", token.Id).Error; err != nil {
		return nil, err
	}
	token.TokenPlanId = &planIdCopy
	return &token, nil
}

// ResetTokenPlanUsage 清零套餐用量并重置窗口锚点。userId <= 0 表示全体用户。
// 锚点归零后，下一次请求会重新落锚点，两个窗口从头开始计时。
func ResetTokenPlanUsage(userId int) (int64, error) {
	now := common.GetTimestamp()
	query := DB.Model(&UserTokenPlan{}).Where("status = ?", TokenPlanStatusActive)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	result := query.Updates(map[string]any{
		"usage_version": gorm.Expr("usage_version + 1"),
		"used_tokens":   0,
		"window1_used":  0,
		"window2_used":  0,
		"window1_index": 0,
		"window2_index": 0,
		"anchor_at":     0,
		"updated_time":  now,
	})
	return result.RowsAffected, result.Error
}

// SyncTokenPlanThrottle 把套餐模板的节流参数推送到该套餐所有生效中的订阅。
//
// 只更新 8 个节流列：绝不触碰额度、窗口时长、锚点、已用量与到期时间。
// 参数是快照语义（下单时冻结），所以收紧限制后需要管理员显式调用本接口才会影响老用户。
func SyncTokenPlanThrottle(planId int) (int64, error) {
	plan, err := GetTokenPlanById(planId)
	if err != nil {
		return 0, err
	}
	plan.NormalizeDefaults()

	updates := plan.throttleUpdates()
	updates["updated_time"] = common.GetTimestamp()

	result := DB.Model(&UserTokenPlan{}).
		Where("plan_id = ? AND status = ? AND end_time > ?", planId, TokenPlanStatusActive, common.GetTimestamp()).
		Updates(updates)
	return result.RowsAffected, result.Error
}

// DeleteTokenPlan 删除套餐模板，并把该套餐所有生效中的订阅一并置为 cancelled。
//
// 与「下架」（Enabled=false）严格区分：下架只影响新购买，已订阅用户照常使用；
// 删除是不可逆的，必须连存量订阅一起取消——否则用户手里会留下一条指向已删除
// 套餐的记录，还在正常消耗额度。
//
// 取消订阅后，其专用 key 仍然绑在 cancelled 的记录上，TokenPlanGuard 会按
// 「套餐已过期」拒绝请求。这里绝不能去清空 tokens.token_plan_id：那会让这把
// unlimited_quota 的 key 退化成普通无限额密钥，等于免费放行。
func DeleteTokenPlan(planId int) (int64, error) {
	if _, err := GetTokenPlanById(planId); err != nil {
		return 0, errors.New("套餐不存在")
	}

	now := common.GetTimestamp()
	var cancelled int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Keep the same template -> subscription lock order as purchase.
		// Otherwise deleting a template while renewing it can deadlock.
		var plan TokenPlan
		if err := lockForUpdate(tx).Where("id = ?", planId).First(&plan).Error; err != nil {
			return err
		}
		result := tx.Model(&UserTokenPlan{}).
			Where("plan_id = ? AND status = ?", planId, TokenPlanStatusActive).
			Updates(map[string]any{
				"status":       TokenPlanStatusCancelled,
				"updated_time": now,
			})
		if result.Error != nil {
			return result.Error
		}
		cancelled = result.RowsAffected
		return tx.Delete(&TokenPlan{}, planId).Error
	})
	if err != nil {
		return 0, err
	}
	return cancelled, nil
}

// CountThrottleDriftByPlan 返回「节流参数与套餐模板不一致的生效订阅数」，按套餐 id 聚合。
//
// 节流参数是快照语义：管理员改模板后，已订阅用户要显式调用 SyncTokenPlanThrottle
// 才会生效。这个计数用于在管理页标出「还有 N 个订阅没同步」。
// 返回的 map 里没有出现的套餐，表示其订阅全部与模板一致（或没有订阅）。
func CountThrottleDriftByPlan() (map[int]int64, error) {
	type driftRow struct {
		PlanId int   `gorm:"column:plan_id"`
		Total  int64 `gorm:"column:total"`
	}
	rows := make([]driftRow, 0)

	err := DB.Table("user_token_plans AS p").
		Select("p.plan_id AS plan_id, COUNT(*) AS total").
		Joins("JOIN token_plans AS t ON t.id = p.plan_id").
		Where("p.status = ? AND p.end_time > ?", TokenPlanStatusActive, common.GetTimestamp()).
		Where("(p.first_byte_delay_enabled <> t.first_byte_delay_enabled" +
			" OR p.first_byte_delay_prompt_threshold <> t.first_byte_delay_prompt_threshold" +
			" OR p.first_byte_delay_probability <> t.first_byte_delay_probability" +
			" OR p.first_byte_delay_min_ms <> t.first_byte_delay_min_ms" +
			" OR p.first_byte_delay_max_ms <> t.first_byte_delay_max_ms" +
			" OR p.concurrency_limit <> t.concurrency_limit" +
			" OR p.rate_limit_count <> t.rate_limit_count" +
			" OR p.rate_limit_window_seconds <> t.rate_limit_window_seconds)").
		Group("p.plan_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make(map[int]int64, len(rows))
	for _, row := range rows {
		out[row.PlanId] = row.Total
	}
	return out, nil
}

// TokenPlanSubscriberView 是管理端「已订阅用户」列表的一行。
type TokenPlanSubscriberView struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	PlanTitle    string `json:"plan_title" gorm:"column:plan_title"`
	Status       string `json:"status" gorm:"column:status"`
	TotalTokens  int64  `json:"total_tokens" gorm:"column:total_tokens"`
	UsedTokens   int64  `json:"used_tokens" gorm:"column:used_tokens"`
	Window1Used  int64  `json:"window1_used" gorm:"column:window1_used"`
	Window1Limit int64  `json:"window1_limit" gorm:"column:window1_limit"`
	Window2Used  int64  `json:"window2_used" gorm:"column:window2_used"`
	Window2Limit int64  `json:"window2_limit" gorm:"column:window2_limit"`
	StartTime    int64  `json:"start_time" gorm:"column:start_time"`
	EndTime      int64  `json:"end_time" gorm:"column:end_time"`
}

// GetTokenPlanSubscribers 返回当前生效中的订阅列表（含用户名），供管理端重置用量。
func GetTokenPlanSubscribers() ([]*TokenPlanSubscriberView, error) {
	now := common.GetTimestamp()
	views := make([]*TokenPlanSubscriberView, 0)
	err := DB.Table("user_token_plans AS p").
		Select("p.user_id, COALESCE(u.username, '') AS username, p.plan_title, p.status, "+
			"p.total_tokens, p.used_tokens, p.window1_used, p.window1_tokens AS window1_limit, "+
			"p.window2_used, p.window2_tokens AS window2_limit, p.start_time, p.end_time").
		Joins("LEFT JOIN users AS u ON u.id = p.user_id").
		Where("p.status = ? AND p.end_time > ?", TokenPlanStatusActive, now).
		Order("p.id desc").
		Scan(&views).Error
	if err != nil {
		return nil, err
	}
	return views, nil
}

// ResetTokenPlanKey 为当前用户重新生成专用 key（原地更新，保持 key id 与套餐绑定不变）。
func ResetTokenPlanKey(userId int) (*Token, error) {
	newKey, err := GenerateTokenPlanKey()
	if err != nil {
		return nil, err
	}
	var token *Token
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userId).UpdateColumn("quota", gorm.Expr("quota")).Error; err != nil {
			return err
		}
		var sub UserTokenPlan
		if err := lockForUpdate(tx).Where("user_id = ? AND status = ? AND end_time > ?", userId, TokenPlanStatusActive, common.GetTimestamp()).Order("id desc").First(&sub).Error; err != nil {
			return err
		}
		var err error
		token, err = bindTokenPlanKey(tx, userId, sub.Id, common.GetTimestamp())
		if err != nil {
			return err
		}
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			return err
		}
		if err := tx.Model(token).Updates(map[string]any{"key": newKey, "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		token.Key = newKey
		return nil
	})
	return token, err
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
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true,
		},
		{
			Title: "PRO", Subtitle: "主力档位，覆盖大部分开发场景",
			PriceAmount: 159, Currency: "CNY", DurationDays: 30,
			TotalTokens:    15_000_000,
			Window1Seconds: DefaultTokenPlanWindow1Seconds, Window1Tokens: 3_000_000,
			Window2Seconds: DefaultTokenPlanWindow2Seconds, Window2Tokens: 9_000_000,
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true,
		},
		{
			Title: "MAX", Subtitle: "高强度使用，长上下文与批量任务",
			PriceAmount: 359, Currency: "CNY", DurationDays: 30,
			TotalTokens:    40_000_000,
			Window1Seconds: DefaultTokenPlanWindow1Seconds, Window1Tokens: 8_000_000,
			Window2Seconds: DefaultTokenPlanWindow2Seconds, Window2Tokens: 25_000_000,
			MeteringMode: TokenPlanMeteringWeighted, Enabled: true,
		},
	}
	for i := range presets {
		if err := DB.Create(&presets[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
