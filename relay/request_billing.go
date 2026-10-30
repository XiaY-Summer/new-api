package relay

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// PrepareRequestBilling estimates and reserves one request's charge. Transports
// provide the current request body through BodyStorage or BillingRequestInput;
// channel retries retain the resulting billing session and pricing snapshot.
func PrepareRequestBilling(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	tokenPlanId := common.GetContextKeyInt(c, constant.ContextKeyTokenPlanId)
	if tokenPlanId > 0 {
		switch info.RelayMode {
		case relayconstant.RelayModeAlphaSearch, relayconstant.RelayModeToolsSearch, relayconstant.RelayModeToolsFetch, relayconstant.RelayModeRealtime:
			return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgTokenPlanEndpointUnsupported)), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}
	meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer}
	if info.Request != nil && (needSensitiveCheck || constant.CountToken || tokenPlanId > 0) {
		meta = info.Request.GetTokenCountMeta()
	} else {
		// Avoid building CombineText when only the pricing quantities are needed.
		switch request := info.Request.(type) {
		case *dto.GeneralOpenAIRequest:
			meta.MaxTokens = int(max(lo.FromPtr(request.MaxTokens), lo.FromPtr(request.MaxCompletionTokens)))
		case *dto.OpenAIResponsesRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxOutputTokens))
		case *dto.ClaudeRequest:
			meta.MaxTokens = int(lo.FromPtr(request.MaxTokens))
		case *dto.ImageRequest:
			meta = request.GetTokenCountMeta()
		}
	}

	if needSensitiveCheck && meta != nil {
		if contains, words := service.CheckSensitiveText(meta.CombineText); contains {
			service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
			message := fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", "))
			logger.LogWarn(c, message)
			return types.NewError(errors.New(message), types.ErrorCodeSensitiveWordsDetected)
		}
	}

	var tokens int
	var err error
	if tokenPlanId > 0 {
		tokens, err = service.CountRequestToken(c, meta, info)
	} else {
		tokens, err = service.EstimateRequestToken(c, meta, info)
	}
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)

	// TOKEN PLAN 专用密钥：不计钱包/订阅，只按 token 计量。
	// 保留上面的 token 估算，上游未返回 usage 时仍需用它兜底。
	if tokenPlanId > 0 {
		info.TokenPlanId = tokenPlanId
		info.TokenPlanMetering = common.GetContextKeyString(c, constant.ContextKeyTokenPlanMetering)
		if apiErr := service.PrepareTokenPlanBilling(c, info, tokens, meta); apiErr != nil {
			return apiErr
		}

		// 首字延迟在这里一次性掷骰子：prompt token 估算刚就绪，且本函数在
		// controller.Relay 的重试循环之外，重试不会重新掷、也不会叠加延迟。
		// 真正的等待发生在 controller.Relay 里、转发上游之前。
		if throttle, ok := common.GetContextKeyType[*model.TokenPlanThrottle](c, constant.ContextKeyTokenPlanThrottle); ok && throttle != nil {
			if delay := throttle.DecideFirstByteDelay(tokens, rand.Float64); delay > 0 {
				info.FirstByteDelayGate = relaycommon.NewFirstByteDelayGate(delay)
			}
		}
		return nil
	}

	priceData, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if priceData.FreeModel {
		logger.LogInfo(c, common.LogText("model %s is free, skipping pre-consume", info.OriginModelName))
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, info)
}

// RefundFailedRequestBilling applies the common final-failure policy after all
// eligible attempts have ended. A settled BillingSession never refunds again.
func RefundFailedRequestBilling(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil {
		return nil
	}
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if info.TokenPlanRequestId != "" {
		if err := model.SettleTokenPlanTokens(info.UserId, info.TokenPlanId, info.TokenPlanRequestId, 0, true); err != nil {
			logger.LogError(c, common.LogText("failed to refund token plan reservation: %s", err.Error()))
		}
		return apiErr
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	service.ChargeViolationFeeIfNeeded(c, info, apiErr)
	return apiErr
}
