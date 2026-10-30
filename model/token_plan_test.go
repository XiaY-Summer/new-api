package model

import (
	"testing"
	"time"
)

const fiveHours int64 = 5 * 60 * 60

func TestWindowIndex(t *testing.T) {
	const anchor int64 = 1_000_000
	cases := []struct {
		name string
		now  int64
		want int64
	}{
		{"before anchor", anchor - 100, 0},
		{"exactly at anchor", anchor, 0},
		{"last second of first window", anchor + fiveHours - 1, 0},
		{"first second of second window", anchor + fiveHours, 1},
		{"deep into third window", anchor + 2*fiveHours + 10, 2},
	}
	for _, tc := range cases {
		if got := WindowIndex(anchor, tc.now, fiveHours); got != tc.want {
			t.Errorf("%s: WindowIndex() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestWindowIndexWithoutAnchor(t *testing.T) {
	if got := WindowIndex(0, 12345, fiveHours); got != 0 {
		t.Errorf("no anchor should stay in window 0, got %d", got)
	}
	if got := WindowIndex(1000, 2000, 0); got != 0 {
		t.Errorf("zero window length should stay in window 0, got %d", got)
	}
}

func TestBuildWindowViewResetsOnNewWindow(t *testing.T) {
	const anchor int64 = 1_000_000
	// 已存窗口序号 0、用量 900；当前仍在窗口 0 内
	same := BuildWindowView("window1", anchor, fiveHours, 1000, 0, 900, anchor+100)
	if same.UsedTokens != 900 || same.Exhausted {
		t.Fatalf("same window: got used=%d exhausted=%v", same.UsedTokens, same.Exhausted)
	}

	// 跨到窗口 1 后，即使存储用量仍是 900，也应视为归零
	next := BuildWindowView("window1", anchor, fiveHours, 1000, 0, 900, anchor+fiveHours+1)
	if next.UsedTokens != 0 || next.Exhausted {
		t.Fatalf("next window: got used=%d exhausted=%v", next.UsedTokens, next.Exhausted)
	}
	if next.ResetAt != anchor+2*fiveHours {
		t.Fatalf("next window reset_at = %d, want %d", next.ResetAt, anchor+2*fiveHours)
	}
}

func TestBuildWindowViewPercentAndExhausted(t *testing.T) {
	const anchor int64 = 1_000_000
	view := BuildWindowView("window2", anchor, 7*24*3600, 1000, 0, 1000, anchor+10)
	if view.UsedPercent != 100 {
		t.Errorf("used percent = %d, want 100", view.UsedPercent)
	}
	if !view.Exhausted {
		t.Error("window should be exhausted when used >= limit")
	}
	if view.RemainingTokens != 0 {
		t.Errorf("remaining = %d, want 0", view.RemainingTokens)
	}

	over := BuildWindowView("window2", anchor, 7*24*3600, 1000, 0, 5000, anchor+10)
	if over.UsedPercent != 100 {
		t.Errorf("percent should clamp to 100, got %d", over.UsedPercent)
	}

	unlimited := BuildWindowView("window2", anchor, 7*24*3600, 0, 0, 5000, anchor+10)
	if unlimited.Exhausted {
		t.Error("limit 0 means unlimited and must never be exhausted")
	}
}

func TestWeightedTokens(t *testing.T) {
	cases := []struct {
		name   string
		detail TokenPlanUsageDetail
		mode   string
		want   int64
	}{
		{
			name:   "weighted by model ratio",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CompletionTokens: 400, ModelRatio: 1.25, CacheRatio: 1},
			mode:   TokenPlanMeteringWeighted,
			want:   1250,
		},
		{
			name:   "weighted rounds half up",
			detail: TokenPlanUsageDetail{PromptTokens: 3, ModelRatio: 0.5, CacheRatio: 1},
			mode:   TokenPlanMeteringWeighted,
			want:   2,
		},
		{
			name:   "raw ignores ratio",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CompletionTokens: 400, ModelRatio: 15, CacheRatio: 1},
			mode:   TokenPlanMeteringRaw,
			want:   1000,
		},
		{
			name:   "unknown ratio falls back to raw",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CompletionTokens: 400, ModelRatio: 0},
			mode:   TokenPlanMeteringWeighted,
			want:   1000,
		},
		{
			name:   "zero tokens",
			detail: TokenPlanUsageDetail{ModelRatio: 1.25},
			mode:   TokenPlanMeteringWeighted,
			want:   0,
		},
		{
			// 缓存命中按 0.5 折：600×2.5 + 400×0.5×2.5 + 200×2.5 = 1500 + 500 + 500
			name:   "cache hit discounted",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CacheTokens: 400, CompletionTokens: 200, ModelRatio: 2.5, CacheRatio: 0.5},
			mode:   TokenPlanMeteringWeighted,
			want:   2500,
		},
		{
			// 缓存写入按 1.25 倍：600×2.5 + 400×1.25×2.5 = 1500 + 1250
			name:   "cache creation premium",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CacheCreationTokens: 400, ModelRatio: 2.5, CacheCreationRatio: 1.25},
			mode:   TokenPlanMeteringWeighted,
			want:   2750,
		},
		{
			// 倍率缺失时按 1 处理，等价于全价计
			name:   "missing cache ratios default to full price",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CacheTokens: 400, ModelRatio: 2.5},
			mode:   TokenPlanMeteringWeighted,
			want:   2500,
		},
		{
			// raw 模式仍应把缓存 token 计入总量
			name:   "raw counts cache tokens",
			detail: TokenPlanUsageDetail{PromptTokens: 600, CacheTokens: 300, CacheCreationTokens: 100, ModelRatio: 2.5, CacheRatio: 0.5},
			mode:   TokenPlanMeteringRaw,
			want:   1000,
		},
	}
	for _, tc := range cases {
		if got := WeightedTokens(tc.detail, tc.mode); got != tc.want {
			t.Errorf("%s: WeightedTokens() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSplitModelLimits(t *testing.T) {
	if got := SplitModelLimits(""); got != nil {
		t.Errorf("empty string should yield nil, got %v", got)
	}
	if got := SplitModelLimits("  ,  "); got != nil {
		t.Errorf("blank entries should yield nil, got %v", got)
	}
	got := SplitModelLimits("gpt-4o, claude-sonnet-4 ,,")
	if len(got) != 2 || got[0] != "gpt-4o" || got[1] != "claude-sonnet-4" {
		t.Errorf("unexpected split result: %v", got)
	}
}

// TestTokenPlanThrottleNormalize 覆盖非法输入收敛：概率越界、上下限倒挂、负数、
// 「设了频率次数却没设窗口」。
func TestTokenPlanThrottleNormalize(t *testing.T) {
	got := TokenPlanThrottle{
		FirstByteDelayEnabled:         true,
		FirstByteDelayPromptThreshold: -5,
		FirstByteDelayProbability:     150,
		FirstByteDelayMinMs:           -100,
		FirstByteDelayMaxMs:           -1,
		ConcurrencyLimit:              -2,
		RateLimitCount:                10,
		RateLimitWindowSeconds:        0,
	}.Normalize()

	if got.FirstByteDelayPromptThreshold != 0 {
		t.Errorf("negative threshold should clamp to 0, got %d", got.FirstByteDelayPromptThreshold)
	}
	if got.FirstByteDelayProbability != 100 {
		t.Errorf("probability should clamp to 100, got %d", got.FirstByteDelayProbability)
	}
	if got.FirstByteDelayMinMs != 0 || got.FirstByteDelayMaxMs != 0 {
		t.Errorf("negative delay should clamp to 0, got %d/%d", got.FirstByteDelayMinMs, got.FirstByteDelayMaxMs)
	}
	if got.ConcurrencyLimit != 0 {
		t.Errorf("negative concurrency should clamp to 0, got %d", got.ConcurrencyLimit)
	}
	if got.RateLimitWindowSeconds != 60 {
		t.Errorf("missing window should default to 60, got %d", got.RateLimitWindowSeconds)
	}

	// 上下限倒挂时抬平，避免随机区间变成负数。
	flipped := TokenPlanThrottle{FirstByteDelayMinMs: 900, FirstByteDelayMaxMs: 100}.Normalize()
	if flipped.FirstByteDelayMaxMs != 900 {
		t.Errorf("max below min should be raised to min, got %d", flipped.FirstByteDelayMaxMs)
	}

	// 未设频率次数时窗口保持 0，表示不限。
	noRate := TokenPlanThrottle{RateLimitCount: 0, RateLimitWindowSeconds: 0}.Normalize()
	if noRate.RateLimitWindowSeconds != 0 {
		t.Errorf("window should stay 0 when no rate limit, got %d", noRate.RateLimitWindowSeconds)
	}
}

// TestDecideFirstByteDelay 覆盖首字延迟的判定顺序：开关 -> 阈值 -> 概率 -> 随机时长。
// roll 注入让每个分支都可确定地命中。
func TestDecideFirstByteDelay(t *testing.T) {
	// fixed 返回常量，用于让随机分支落在确定位置。
	fixed := func(v float64) func() float64 {
		return func() float64 { return v }
	}

	cases := []struct {
		name   string
		t      TokenPlanThrottle
		prompt int
		roll   func() float64
		want   time.Duration
	}{
		{
			name: "disabled",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: false, FirstByteDelayProbability: 100, FirstByteDelayMaxMs: 500},
			want: 0,
		},
		{
			name:   "prompt below threshold",
			t:      TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayPromptThreshold: 200, FirstByteDelayProbability: 100, FirstByteDelayMaxMs: 500},
			prompt: 199,
			want:   0,
		},
		{
			name:   "prompt exactly at threshold",
			t:      TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayPromptThreshold: 200, FirstByteDelayProbability: 100, FirstByteDelayMinMs: 500, FirstByteDelayMaxMs: 500},
			prompt: 200,
			roll:   fixed(0),
			want:   500 * time.Millisecond,
		},
		{
			name:   "threshold zero always triggers",
			t:      TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 100, FirstByteDelayMinMs: 300, FirstByteDelayMaxMs: 800},
			prompt: 0,
			roll:   fixed(0),
			want:   300 * time.Millisecond,
		},
		{
			name: "probability zero never triggers",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 0, FirstByteDelayMaxMs: 500},
			roll: fixed(0),
			want: 0,
		},
		{
			name: "probability fifty hits",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 50, FirstByteDelayMinMs: 100, FirstByteDelayMaxMs: 100},
			roll: fixed(0.2),
			want: 100 * time.Millisecond,
		},
		{
			name: "probability fifty misses",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 50, FirstByteDelayMinMs: 100, FirstByteDelayMaxMs: 100},
			roll: fixed(0.7),
			want: 0,
		},
		{
			name: "range upper bound",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 100, FirstByteDelayMinMs: 300, FirstByteDelayMaxMs: 800},
			// 概率判定消耗一次 roll，时长判定消耗第二次。
			roll: func() float64 { return 0.999 },
			want: 800 * time.Millisecond,
		},
		{
			name: "max zero never triggers",
			t:    TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayProbability: 100, FirstByteDelayMinMs: 0, FirstByteDelayMaxMs: 0},
			roll: fixed(0),
			want: 0,
		},
	}

	for _, tc := range cases {
		roll := tc.roll
		if roll == nil {
			roll = fixed(0)
		}
		if got := tc.t.DecideFirstByteDelay(tc.prompt, roll); got != tc.want {
			t.Errorf("%s: DecideFirstByteDelay() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestTokenPlanThrottleSnapshot 确认模板与用户快照的节流字段一一对应，
// 加字段时漏掉任一侧都会被这个用例抓住。
func TestTokenPlanThrottleSnapshot(t *testing.T) {
	plan := &TokenPlan{
		FirstByteDelayEnabled:         true,
		FirstByteDelayPromptThreshold: 4000,
		FirstByteDelayProbability:     70,
		FirstByteDelayMinMs:           200,
		FirstByteDelayMaxMs:           900,
		ConcurrencyLimit:              3,
		RateLimitCount:                20,
		RateLimitWindowSeconds:        60,
	}

	want := plan.Throttle()
	if got := (&UserTokenPlan{
		FirstByteDelayEnabled:         plan.FirstByteDelayEnabled,
		FirstByteDelayPromptThreshold: plan.FirstByteDelayPromptThreshold,
		FirstByteDelayProbability:     plan.FirstByteDelayProbability,
		FirstByteDelayMinMs:           plan.FirstByteDelayMinMs,
		FirstByteDelayMaxMs:           plan.FirstByteDelayMaxMs,
		ConcurrencyLimit:              plan.ConcurrencyLimit,
		RateLimitCount:                plan.RateLimitCount,
		RateLimitWindowSeconds:        plan.RateLimitWindowSeconds,
	}).Throttle(); got != want {
		t.Errorf("throttle snapshot mismatch:/n got %+v\nwant %+v", got, want)
	}

	// nil 接收者不应 panic。
	if got := (*TokenPlan)(nil).Throttle(); got != (TokenPlanThrottle{}) {
		t.Errorf("nil plan should yield zero throttle, got %+v", got)
	}

	// applyThrottle 应把模板值完整写进快照。
	var sub UserTokenPlan
	sub.applyThrottle(plan)
	if sub.Throttle() != want {
		t.Errorf("applyThrottle mismatch:/n got %+v\nwant %+v", sub.Throttle(), want)
	}
}
