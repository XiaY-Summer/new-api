package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// TestTokenPlanViewThrottleRoundTrip 校验 DTO <-> 模型的往返一致。
//
// 节流字段分散在 TokenPlanView、newTokenPlanView、toModel、AdminUpdateTokenPlan 的
// updates map 四处，加字段时漏掉任何一处都会表现为「新建能存、编辑存不住」。
// 这个用例至少能兜住前三处。
func TestTokenPlanViewThrottleRoundTrip(t *testing.T) {
	view := TokenPlanView{
		Id:             7,
		Title:          "PRO",
		Subtitle:       "mainstream",
		PriceAmount:    159,
		Currency:       "CNY",
		DurationDays:   30,
		TotalTokens:    15_000_000,
		Window1Minutes: 300,
		Window1Tokens:  3_000_000,
		Window2Minutes: 10080,
		Window2Tokens:  9_000_000,
		MeteringMode:   model.TokenPlanMeteringWeighted,
		Enabled:        true,

		FirstByteDelayEnabled:         true,
		FirstByteDelayPromptThreshold: 4000,
		FirstByteDelayProbability:     70,
		FirstByteDelayMinMs:           300,
		FirstByteDelayMaxMs:           900,
		ConcurrencyLimit:              3,
		RateLimitCount:                20,
		RateLimitWindowSeconds:        60,
	}

	plan := view.toModel()
	plan.NormalizeDefaults()
	got := newTokenPlanView(&plan)

	if got.FirstByteDelayEnabled != view.FirstByteDelayEnabled {
		t.Errorf("FirstByteDelayEnabled = %v, want %v", got.FirstByteDelayEnabled, view.FirstByteDelayEnabled)
	}
	if got.FirstByteDelayPromptThreshold != view.FirstByteDelayPromptThreshold {
		t.Errorf("FirstByteDelayPromptThreshold = %d, want %d", got.FirstByteDelayPromptThreshold, view.FirstByteDelayPromptThreshold)
	}
	if got.FirstByteDelayProbability != view.FirstByteDelayProbability {
		t.Errorf("FirstByteDelayProbability = %d, want %d", got.FirstByteDelayProbability, view.FirstByteDelayProbability)
	}
	if got.FirstByteDelayMinMs != view.FirstByteDelayMinMs {
		t.Errorf("FirstByteDelayMinMs = %d, want %d", got.FirstByteDelayMinMs, view.FirstByteDelayMinMs)
	}
	if got.FirstByteDelayMaxMs != view.FirstByteDelayMaxMs {
		t.Errorf("FirstByteDelayMaxMs = %d, want %d", got.FirstByteDelayMaxMs, view.FirstByteDelayMaxMs)
	}
	if got.ConcurrencyLimit != view.ConcurrencyLimit {
		t.Errorf("ConcurrencyLimit = %d, want %d", got.ConcurrencyLimit, view.ConcurrencyLimit)
	}
	if got.RateLimitCount != view.RateLimitCount {
		t.Errorf("RateLimitCount = %d, want %d", got.RateLimitCount, view.RateLimitCount)
	}
	if got.RateLimitWindowSeconds != view.RateLimitWindowSeconds {
		t.Errorf("RateLimitWindowSeconds = %d, want %d", got.RateLimitWindowSeconds, view.RateLimitWindowSeconds)
	}

	// 窗口时长的分钟 <-> 秒换算不能被节流改动影响。
	if got.Window1Minutes != view.Window1Minutes || got.Window2Minutes != view.Window2Minutes {
		t.Errorf("window minutes round trip broken: got %d/%d, want %d/%d",
			got.Window1Minutes, got.Window2Minutes, view.Window1Minutes, view.Window2Minutes)
	}
}

// TestValidateTokenPlanThrottle 校验节流参数在归一化之前就被挡下，
// 而不是悄悄夹到合法范围。
func TestValidateTokenPlanThrottle(t *testing.T) {
	cases := []struct {
		name string
		view TokenPlanView
		want string
	}{
		{"valid", TokenPlanView{FirstByteDelayEnabled: true, FirstByteDelayProbability: 50, FirstByteDelayMinMs: 100, FirstByteDelayMaxMs: 200}, ""},
		{"probability too high", TokenPlanView{FirstByteDelayProbability: 150}, "Trigger probability must be between 0 and 100"},
		{"probability negative", TokenPlanView{FirstByteDelayProbability: -1}, "Trigger probability must be between 0 and 100"},
		{"max below min", TokenPlanView{FirstByteDelayMinMs: 500, FirstByteDelayMaxMs: 100}, "Delay max must be greater than or equal to delay min"},
		{"enabled without delay", TokenPlanView{FirstByteDelayEnabled: true, FirstByteDelayMaxMs: 0}, "Delay max must be greater than 0 when the delay is enabled"},
		{"negative concurrency", TokenPlanView{ConcurrencyLimit: -1}, "Concurrency limit cannot be negative"},
		{"rate without window", TokenPlanView{RateLimitCount: 10}, "Rate limit window must be greater than 0 when a request limit is set"},
	}

	for _, tc := range cases {
		if got := validateTokenPlanThrottle(tc.view); got != tc.want {
			t.Errorf("%s: validateTokenPlanThrottle() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
