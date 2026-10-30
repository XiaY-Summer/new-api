package middleware

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// TOKEN PLAN 的运行时节流：并发上限与频率上限。
//
// 计数 key 用 UserTokenPlan.Id 而不是 user_id：用户升级套餐会新建一条记录，
// 旧计数器随之失联（LRU 自然淘汰），不会把旧套餐的在途请求算进新套餐。
// 一个用户同一时刻最多一条 active 记录，所以正常路径下两者等价。

func tokenPlanConcurrencyKey(sub *model.UserTokenPlan) string {
	return fmt.Sprintf("tokenplan:conc:%d", sub.Id)
}

func tokenPlanRateKey(sub *model.UserTokenPlan) string {
	return fmt.Sprintf("tokenplan:rate:%d", sub.Id)
}

// tokenPlanLimiter 复用包级的 InMemoryRateLimiter 单例。
// Init 是幂等的（store 非 nil 直接返回），但 Request/Reserve 在未 Init 时会写 nil map，
// 所以这里统一兜一次。
func tokenPlanLimiter() *common.InMemoryRateLimiter {
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return &inMemoryRateLimiter
}

// tokenPlanConcurrencyAcquire 申请一个并发槽位，返回释放函数；nil 表示已达上限。
//
// 复用 InMemoryRateLimiter 的 Reserve：它准入时同时看「窗口内已接受数 + 在途预约数」，
// 这里恒传 Complete(false)，窗口永远不增长，于是退化成纯 in-flight 计数器。
// duration 只影响 key 的空闲回收，取 1 小时足够。
//
// 已知局限：多实例部署时实际上限是 N × limit（内存计数按实例隔离）。这是放宽而非
// 误杀；将来要严格上限，只改本函数体（Redis ZSET + lease）即可，调用方无需变动。
func tokenPlanConcurrencyAcquire(sub *model.UserTokenPlan, limit int) func() {
	if sub == nil || limit <= 0 {
		return func() {}
	}
	reservation := tokenPlanLimiter().Reserve(tokenPlanConcurrencyKey(sub), limit, 3600)
	if reservation == nil {
		return nil
	}
	// Complete 内部有 sync.Once，重复调用安全；传 false 只释放槽位、不记入窗口。
	return func() { reservation.Complete(false) }
}

// tokenPlanRateAllow 判定频率限制。allowed=false 时 retryAfter 是建议的重试秒数。
func tokenPlanRateAllow(c *gin.Context, sub *model.UserTokenPlan, throttle model.TokenPlanThrottle) (bool, int64) {
	if sub == nil || throttle.RateLimitCount <= 0 || throttle.RateLimitWindowSeconds <= 0 {
		return true, 0
	}

	key := tokenPlanRateKey(sub)
	if common.RedisEnabled && common.RDB != nil {
		// 固定窗口在 Redis 里是原子的，多实例下也精确。
		allowed, _, ttl, err := redisFixedWindowTake(c.Request.Context(), key, throttle.RateLimitCount, throttle.RateLimitWindowSeconds)
		if err != nil {
			// 软节流：Redis 抖动不该把付费用户挡在门外。
			logger.LogError(c.Request.Context(), common.LogText("token plan rate limit check failed: %s", err.Error()))
			return true, 0
		}
		return allowed, ttl
	}

	if tokenPlanLimiter().Request(key, throttle.RateLimitCount, throttle.RateLimitWindowSeconds) {
		return true, 0
	}
	// 内存版拿不到窗口剩余时间，用整个窗口做上界。
	return false, throttle.RateLimitWindowSeconds
}
