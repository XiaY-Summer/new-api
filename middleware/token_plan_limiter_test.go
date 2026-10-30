package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 每个用例用不同的 UserTokenPlan.Id，避免与同包其他测试或彼此之间共享计数。
const (
	concTestPlanId  = 9_100_001
	concOtherPlanId = 9_100_002
	rateTestPlanId  = 9_100_003
	rateOtherPlanId = 9_100_004
)

func TestTokenPlanConcurrencyAcquire(t *testing.T) {
	sub := &model.UserTokenPlan{Id: concTestPlanId}

	// limit <= 0 表示不限：返回可调用的空函数，而不是 nil（nil 表示已满）。
	if release := tokenPlanConcurrencyAcquire(sub, 0); release == nil {
		t.Fatal("limit 0 should be unlimited, got nil release")
	} else {
		release()
	}

	release1 := tokenPlanConcurrencyAcquire(sub, 2)
	release2 := tokenPlanConcurrencyAcquire(sub, 2)
	if release1 == nil || release2 == nil {
		t.Fatal("first two acquisitions should succeed with limit 2")
	}
	if release := tokenPlanConcurrencyAcquire(sub, 2); release != nil {
		t.Fatal("third acquisition should be rejected with limit 2")
	}

	// 释放一个后应能再进一个。
	release1()
	if release := tokenPlanConcurrencyAcquire(sub, 2); release == nil {
		t.Fatal("slot should be reusable after release")
	} else {
		release()
	}

	// 重复释放不能把计数减成负数（Complete 内部有 sync.Once）。
	release1()
	release1()
	release3 := tokenPlanConcurrencyAcquire(sub, 2)
	if release3 == nil {
		t.Fatal("double release must not free extra slots")
	}
	release3()
	release2()

	// 不同套餐实例互不影响。
	other := &model.UserTokenPlan{Id: concOtherPlanId}
	releaseOther := tokenPlanConcurrencyAcquire(other, 1)
	if releaseOther == nil {
		t.Fatal("other plan should have its own slot")
	}
	releaseOther()
}

func TestTokenPlanRateAllow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	sub := &model.UserTokenPlan{Id: rateTestPlanId}
	throttle := model.TokenPlanThrottle{RateLimitCount: 3, RateLimitWindowSeconds: 60}

	for i := 0; i < 3; i++ {
		if allowed, _ := tokenPlanRateAllow(c, sub, throttle); !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	allowed, retryAfter := tokenPlanRateAllow(c, sub, throttle)
	if allowed {
		t.Fatal("fourth request should be rejected")
	}
	if retryAfter <= 0 {
		t.Errorf("rejection should suggest a retry delay, got %d", retryAfter)
	}

	// 另一个套餐实例的窗口独立。
	other := &model.UserTokenPlan{Id: rateOtherPlanId}
	if allowed, _ := tokenPlanRateAllow(c, other, throttle); !allowed {
		t.Fatal("other plan should not share the window")
	}

	// 未配置频率限制时一律放行。
	unlimited := &model.UserTokenPlan{Id: 9_100_005}
	for i := 0; i < 5; i++ {
		if allowed, _ := tokenPlanRateAllow(c, unlimited, model.TokenPlanThrottle{}); !allowed {
			t.Fatal("unconfigured rate limit should always allow")
		}
	}
}
