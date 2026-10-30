package service

import (
	"errors"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func tokenPlanRates(modelName string) hosttypes.PriceData {
	rates := hosttypes.PriceData{}
	rates.ModelRatio, _, _ = ratio_setting.GetModelRatio(modelName)
	rates.CacheRatio, _ = ratio_setting.GetCacheRatio(modelName)
	rates.CacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(modelName)
	if math.IsNaN(rates.ModelRatio) || math.IsInf(rates.ModelRatio, 0) {
		rates.ModelRatio = 0
	}
	for _, rate := range []*float64{&rates.CacheRatio, &rates.CacheCreationRatio} {
		if *rate <= 0 || math.IsNaN(*rate) || math.IsInf(*rate, 0) {
			*rate = 1
		}
	}
	return rates
}

func PrepareTokenPlanBilling(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) *types.NewAPIError {
	if meta.MaxTokens <= 0 {
		switch info.Request.(type) {
		case *dto.GeneralOpenAIRequest, *dto.OpenAIResponsesRequest, *dto.ClaudeRequest, *dto.GeminiChatRequest:
			if info.RelayMode != relayconstant.RelayModeEmbeddings && info.RelayMode != relayconstant.RelayModeModerations {
				return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgTokenPlanOutputLimitRequired)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
	}
	info.TokenPlanRates = tokenPlanRates(info.GetBillingModelName())
	outputCount := 1.0
	switch request := info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		if request.N != nil {
			outputCount = float64(max(*request.N, 1))
		}
	case *dto.GeminiChatRequest:
		if request.GenerationConfig.CandidateCount != nil {
			outputCount = float64(max(*request.GenerationConfig.CandidateCount, 1))
		}
	case *dto.ImageRequest:
		if request.N != nil {
			outputCount = max(float64(*request.N), 1)
		}
	}
	output, clamp := common.QuotaRoundChecked(float64(meta.MaxTokens) * outputCount)
	if clamp != nil {
		noteQuotaClamp(info, clamp)
		return types.NewErrorWithStatusCode(clamp, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// Reserve cache creation at its worst configured rate. Actual cache hits
	// are discounted only after the upstream returns its usage breakdown.
	detail := model.TokenPlanUsageDetail{
		CacheCreationTokens: promptTokens, CompletionTokens: output,
		ModelRatio:         info.TokenPlanRates.ModelRatio,
		CacheCreationRatio: max(1, info.TokenPlanRates.CacheRatio, info.TokenPlanRates.CacheCreationRatio),
	}
	amount, clamp := model.WeightedTokensChecked(detail, info.TokenPlanMetering)
	if clamp != nil {
		noteQuotaClamp(info, clamp)
		return types.NewErrorWithStatusCode(clamp, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	reserveRemaining := meta.MaxTokens <= 0
	if _, image := info.Request.(*dto.ImageRequest); image {
		reserveRemaining = true
	}
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings, relayconstant.RelayModeRerank, relayconstant.RelayModeModerations:
		reserveRemaining = false
	}
	requestId := common.GetUUID()
	record, err := model.ReserveTokenPlanTokens(info.UserId, info.TokenPlanId, requestId, amount, reserveRemaining)
	if err != nil {
		if errors.Is(err, model.ErrTokenPlanQuotaExhausted) || errors.Is(err, model.ErrTokenPlanUnavailable) {
			return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgTokenPlanTotalExhausted)), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	info.TokenPlanRequestId = requestId
	info.TokenPlanPreConsumed = record.Tokens
	return nil
}

// SettleTokenPlanUsage is shared by text and audio. It never charges the wallet.
func SettleTokenPlanUsage(info *relaycommon.RelayInfo, detail model.TokenPlanUsageDetail) error {
	if info.TokenPlanId <= 0 {
		return nil
	}
	rates := info.TokenPlanRates
	if info.TokenPlanRequestId == "" {
		rates = tokenPlanRates(info.GetBillingModelName())
	}
	info.TokenPlanRates = rates
	detail.ModelRatio, detail.CacheRatio, detail.CacheCreationRatio = rates.ModelRatio, rates.CacheRatio, rates.CacheCreationRatio
	tokens, clamp := model.WeightedTokensChecked(detail, info.TokenPlanMetering)
	noteQuotaClamp(info, clamp)
	info.TokenPlanConsumed = tokens
	if info.TokenPlanRequestId == "" {
		return model.RecordUsage(info.UserId, info.TokenPlanId, detail, info.TokenPlanMetering)
	}
	return model.SettleTokenPlanTokens(info.UserId, info.TokenPlanId, info.TokenPlanRequestId, tokens, false)
}
