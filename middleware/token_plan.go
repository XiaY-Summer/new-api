package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// TokenPlanGuard 是 TOKEN PLAN 专用密钥的请求前闸门。
//
// 只读实现：窗口翻篇与否通过「存储的窗口序号 vs 当前序号」在内存里推导，
// 不写库。真正的用量累加发生在结算阶段（model.RecordUsage）。
// 普通 key 在 SetupContextForToken 阶段就不会写入 TokenPlanId，因此这里直接放行。
func TokenPlanGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		planId := common.GetContextKeyInt(c, constant.ContextKeyTokenPlanId)
		if planId <= 0 {
			c.Next()
			return
		}

		sub, err := model.GetUserTokenPlanById(planId)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, common.TranslateMessage(c, i18n.MsgDatabaseError))
			return
		}
		if sub == nil {
			abortWithOpenAiMessage(c, http.StatusForbidden,
				i18n.T(c, i18n.MsgTokenPlanNotFound), types.ErrorCodeInsufficientUserQuota)
			return
		}

		now := common.GetTimestamp()
		if sub.IsExpired(now) {
			abortWithOpenAiMessage(c, http.StatusForbidden,
				i18n.T(c, i18n.MsgTokenPlanExpired), types.ErrorCodeInsufficientUserQuota)
			return
		}

		if sub.TotalTokens > 0 && sub.UsedTokens >= sub.TotalTokens {
			abortWithOpenAiMessage(c, http.StatusForbidden,
				i18n.T(c, i18n.MsgTokenPlanTotalExhausted), types.ErrorCodeInsufficientUserQuota)
			return
		}

		window1, window2 := sub.WindowViews(now)
		for _, w := range []model.TokenPlanWindowView{window1, window2} {
			if !w.Exhausted {
				continue
			}
			abortWithOpenAiMessage(c, http.StatusTooManyRequests,
				i18n.T(c, i18n.MsgTokenPlanWindowExhausted, map[string]any{
					"Window":  formatWindowLabel(w.Seconds),
					"ResetAt": formatResetTime(w.ResetAt),
				}),
				types.ErrorCodeInsufficientUserQuota,
			)
			return
		}

		// 分组限制：套餐限定了分组时，请求实际使用的分组必须在列表内。
		// 分组由 TokenAuth 在进入本中间件前写入 context。
		if allowed := model.SplitModelLimits(sub.GroupLimits); len(allowed) > 0 {
			usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
			matched := false
			for _, group := range allowed {
				if group == usingGroup {
					matched = true
					break
				}
			}
			if !matched {
				abortWithOpenAiMessage(c, http.StatusForbidden,
					i18n.T(c, i18n.MsgTokenPlanGroupForbidden, map[string]any{"Group": usingGroup}),
					types.ErrorCodeInsufficientUserQuota)
				return
			}
		}

		throttle := sub.Throttle()

		// 频率限制：无状态、最便宜，放在并发之前——被频率拒掉的请求不该占用并发槽位。
		if allowed, retryAfter := tokenPlanRateAllow(c, sub, throttle); !allowed {
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			}
			abortWithOpenAiMessage(c, http.StatusTooManyRequests,
				i18n.T(c, i18n.MsgTokenPlanRateLimited, map[string]any{
					"Window": formatWindowLabel(throttle.RateLimitWindowSeconds),
					"Max":    throttle.RateLimitCount,
				}),
				types.ErrorCodeInsufficientUserQuota)
			return
		}

		// 并发槽位必须在所有拒绝分支之后申请，否则一个注定被 403 的请求会白占槽位。
		release := tokenPlanConcurrencyAcquire(sub, throttle.ConcurrencyLimit)
		if release == nil {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests,
				i18n.T(c, i18n.MsgTokenPlanConcurrencyLimited, map[string]any{
					"Max": throttle.ConcurrencyLimit,
				}),
				types.ErrorCodeInsufficientUserQuota)
			return
		}
		// c.Next() 会一直阻塞到 handler 链跑完（流式即整条流结束）才返回，所以这里的
		// defer 恰好等于「请求真正结束时释放」。handler panic 时栈展开同样经过本帧。
		defer release()

		common.SetContextKey(c, constant.ContextKeyTokenPlanMetering, sub.MeteringMode)
		common.SetContextKey(c, constant.ContextKeyTokenPlanThrottle, &throttle)
		c.Next()
	}
}

// TokenPlanReject 拒绝 TOKEN PLAN 专用密钥访问不做 token 计量的端点（例如 /mj）。
//
// 这些端点走异步任务计价，不经过 PrepareRequestBilling 的 token 计量；如果只挂
// TokenPlanGuard，就变成「拦额度但不记账」，等于给套餐 key 开了一条免费通道。
func TokenPlanReject() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.GetContextKeyInt(c, constant.ContextKeyTokenPlanId) > 0 {
			abortWithOpenAiMessage(c, http.StatusForbidden,
				i18n.T(c, i18n.MsgTokenPlanEndpointUnsupported), types.ErrorCodeInsufficientUserQuota)
			return
		}
		c.Next()
	}
}

// formatWindowLabel 把窗口秒数转成人类可读的档位标签，例如 5h / 7d。
func formatWindowLabel(seconds int64) string {
	switch {
	case seconds <= 0:
		return "-"
	case seconds%(24*3600) == 0:
		return fmt.Sprintf("%dd", seconds/(24*3600))
	case seconds%3600 == 0:
		return fmt.Sprintf("%dh", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func formatResetTime(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02 15:04")
}
