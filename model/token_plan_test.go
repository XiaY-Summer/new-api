package model

import "testing"

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
