package middleware

import (
	"fmt"
	"net/http"
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

		common.SetContextKey(c, constant.ContextKeyTokenPlanMetering, sub.MeteringMode)
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
