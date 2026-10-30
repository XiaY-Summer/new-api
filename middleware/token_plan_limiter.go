package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	return fmt.Sprintf("tokenplan:rate:v2:%d", sub.Id)
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
// With Redis, leases are shared by every replica and renewed for the lifetime
// of the request. A failed renewal cancels the request instead of failing open.
func tokenPlanConcurrencyAcquire(sub *model.UserTokenPlan, limit int, c *gin.Context) func() {
	if sub == nil || limit <= 0 {
		return func() {}
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return nil
		}
		const leaseMs = 300_000
		const acquireScript = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[1]) then return 0 end
redis.call('ZADD', KEYS[1], now + ARGV[2], ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1`
		const renewScript = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local expiry = redis.call('ZSCORE', KEYS[1], ARGV[2])
if not expiry or tonumber(expiry) <= now then return 0 end
redis.call('ZADD', KEYS[1], now + ARGV[1], ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[1])
return 1`
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		ctx, cancel := context.WithCancel(ctx)
		key, member := tokenPlanConcurrencyKey(sub), common.GetUUID()
		client := common.RDB
		accepted, err := client.Eval(ctx, acquireScript, []string{key}, limit, leaseMs, member).Int()
		if err != nil || accepted != 1 {
			cancel()
			if err != nil {
				logger.LogError(ctx, common.LogText("token plan concurrency check failed: %s", err.Error()))
			}
			return nil
		}
		if c != nil && c.Request != nil {
			c.Request = c.Request.WithContext(ctx)
		}
		stop := make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					live, err := client.Eval(ctx, renewScript, []string{key}, leaseMs, member).Int()
					if err != nil || live != 1 {
						logger.LogError(ctx, common.LogText("token plan concurrency lease lost"))
						cancel()
						return
					}
				}
			}
		}()
		var once sync.Once
		return func() {
			once.Do(func() {
				close(stop)
				cancel()
				cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				if err := client.ZRem(cleanup, key, member).Err(); err != nil {
					logger.LogError(cleanup, common.LogText("token plan concurrency release failed: %s", err.Error()))
				}
			})
		}
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
	if common.RedisEnabled {
		if common.RDB == nil {
			return false, 1
		}
		// Match the in-memory sliding window, using Redis time so replicas
		// cannot disagree when their local clocks differ.
		const script = `
local t = redis.call('TIME')
local now = tonumber(t[1])
local window = tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[1]) then
  local first = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, math.max(1, tonumber(first[2]) + window - now)}
end
redis.call('ZADD', KEYS[1], now, ARGV[3])
redis.call('EXPIRE', KEYS[1], window)
return {1, 0}`
		values, err := common.RDB.Eval(c.Request.Context(), script, []string{key}, throttle.RateLimitCount, throttle.RateLimitWindowSeconds, common.GetUUID()).Slice()
		if err != nil {
			logger.LogError(c.Request.Context(), common.LogText("token plan rate limit check failed: %s", err.Error()))
			return false, 1
		}
		if len(values) != 2 {
			return false, 1
		}
		allowed, err := redisReplyInteger(values[0])
		if err != nil {
			return false, 1
		}
		ttl, err := redisReplyInteger(values[1])
		if err != nil {
			return false, 1
		}
		return allowed == 1, ttl
	}

	if tokenPlanLimiter().Request(key, throttle.RateLimitCount, throttle.RateLimitWindowSeconds) {
		return true, 0
	}
	// 内存版拿不到窗口剩余时间，用整个窗口做上界。
	return false, throttle.RateLimitWindowSeconds
}
