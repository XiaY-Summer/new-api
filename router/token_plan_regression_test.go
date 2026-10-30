package router

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// External DSNs must name isolated test databases: this matrix recreates the
// affected tables to verify both fresh schema and upgrades with ordinary keys.
func TestTokenPlanRegression(t *testing.T) {
	for _, dialect := range []struct{ name, env string }{
		{"sqlite", ""}, {"mysql", "TOKEN_PLAN_TEST_MYSQL_DSN"}, {"postgres", "TOKEN_PLAN_TEST_POSTGRES_DSN"},
	} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect.name, upgrade), func(t *testing.T) {
				if dialect.env != "" && os.Getenv(dialect.env) == "" {
					t.Skip(dialect.env + " is not configured")
				}
				setupTokenPlanRegressionDB(t, os.Getenv(dialect.env), upgrade)
				runTokenPlanRegressionCases(t)
			})
		}
	}
}

func setupTokenPlanRegressionDB(t *testing.T, dsn string, upgrade bool) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousMemory, previousRedis, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath
	previousRate, previousCount := setting.ModelRequestRateLimitEnabled, constant.CountToken
	previousStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = false, false, false
	setting.ModelRequestRateLimitEnabled = false
	common.SQLitePath = filepath.Join(t.TempDir(), "token-plan.db")
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", "")
	t.Cleanup(func() {
		if model.DB != nil && model.DB != previousDB {
			if sqlDB, err := model.DB.DB(); err == nil {
				require.NoError(t, sqlDB.Close())
			}
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath = previousMaster, previousMemory, previousRedis, previousSQLite
		common.SetDatabaseTypes(previousType, previousLogType)
		setting.ModelRequestRateLimitEnabled, constant.CountToken = previousRate, previousCount
		constant.StreamingTimeout = previousStreamingTimeout
	})
	require.NoError(t, model.InitDB())
	defer func() { common.IsMasterNode = false }()
	for _, table := range []any{&model.TokenPlanPreConsumeRecord{}, &model.Token{}, &model.UserTokenPlan{}, &model.TokenPlan{}, &model.User{}, &model.Log{}} {
		require.NoError(t, model.DB.Migrator().DropTable(table))
	}
	var ordinaryId int
	if upgrade {
		require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.User{}, &model.TokenPlan{}, &model.UserTokenPlan{}))
		legacy := &model.Token{UserId: 123, Key: "ordinary-key-survives-upgrade", Name: "ordinary", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 789}
		require.NoError(t, model.DB.Create(legacy).Error)
		ordinaryId = legacy.Id
		require.NoError(t, model.DB.Migrator().DropColumn(&model.Token{}, "token_plan_id"))
		require.NoError(t, model.DB.Migrator().DropColumn(&model.UserTokenPlan{}, "usage_version"))
	}
	for range 2 {
		sqlDB, err := model.DB.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
		common.IsMasterNode = true
		require.NoError(t, model.InitDB())
	}
	common.IsMasterNode = false
	model.LOG_DB = model.DB
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		sqlDB.SetMaxOpenConns(1)
	}
	versionQuery := "SELECT version()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s", version)
	if upgrade {
		var legacy model.Token
		require.NoError(t, db.First(&legacy, ordinaryId).Error)
		assert.Equal(t, "ordinary-key-survives-upgrade", legacy.Key)
		assert.Equal(t, 789, legacy.RemainQuota)
		assert.Nil(t, legacy.TokenPlanId)
	}
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
}

func newTokenPlanRegressionSubscription(t *testing.T) (*model.User, *model.UserTokenPlan, *model.Token) {
	t.Helper()
	user := &model.User{Username: "plan-" + common.GetUUID()[:12], AffCode: common.GetUUID()[:8], Status: common.UserStatusEnabled, Group: "default", Quota: 1000}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.TokenPlan{Title: "Regression", Enabled: true, TotalTokens: 100, ModelLimits: "review-allowed", DurationDays: 30, PriceAmount: 59, MeteringMode: model.TokenPlanMeteringRaw}
	plan.NormalizeDefaults()
	require.NoError(t, model.DB.Create(plan).Error)
	sub, key, err := model.PurchaseTokenPlan(user.Id, plan.Id)
	require.NoError(t, err)
	return user, sub, key
}

func runTokenPlanRegressionCases(t *testing.T) {
	t.Run("text JSON and SSE settle actual tokens", func(t *testing.T) {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
				user, sub, key := newTokenPlanRegressionSubscription(t)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"chat-test\",\"object\":\"chat.completion.chunk\",\"model\":\"review-allowed\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n")
						fmt.Fprint(w, "data: {\"id\":\"chat-test\",\"object\":\"chat.completion.chunk\",\"model\":\"review-allowed\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":6,\"total_tokens\":11}}\n\ndata: [DONE]\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":"chat-test","object":"chat.completion","model":"review-allowed","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`)
				}))
				defer upstream.Close()
				channel := &model.Channel{Name: "plan-upstream", Type: constant.ChannelTypeOpenAI, Key: "test", Status: common.ChannelStatusEnabled, Models: "review-allowed", Group: "default", BaseURL: &upstream.URL}
				require.NoError(t, model.DB.Create(channel).Error)
				require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "review-allowed", ChannelId: channel.Id, Enabled: true}).Error)
				defer func() {
					require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
					require.NoError(t, model.DB.Delete(channel).Error)
				}()
				engine := gin.New()
				SetRelayRouter(engine)
				r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"review-allowed","max_tokens":20,"stream":%v,"messages":[{"role":"user","content":"hello"}]}`, stream)))
				r.Header.Set("Authorization", "Bearer "+key.Key)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, r)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				updated, err := model.GetUserTokenPlanById(sub.Id)
				require.NoError(t, err)
				assert.Equal(t, int64(11), updated.UsedTokens)
				assert.Equal(t, int64(11), updated.Window1Used)
				var wallet model.User
				require.NoError(t, model.DB.First(&wallet, user.Id).Error)
				assert.Equal(t, user.Quota, wallet.Quota)
			})
		}
	})
	t.Run("expired routes", func(t *testing.T) {
		_, sub, key := newTokenPlanRegressionSubscription(t)
		require.NoError(t, model.DB.Model(sub).Update("end_time", common.GetTimestamp()-1).Error)
		engine := gin.New()
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetTaskRouter(engine)
		SetVideoRouter(engine)
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/images/generations", "/v1/images/edits", "/v1/videos", "/v1/tasks/example", "/v1/video/generations"} {
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"review-allowed","input":"hi","prompt":"hi","messages":[{"role":"user","content":"hi"}]}`))
			r.Header.Set("Authorization", "Bearer "+key.Key)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			assert.Equal(t, http.StatusForbidden, w.Code, "%s: %s", path, w.Body.String())
		}
	})
	t.Run("model whitelist", func(t *testing.T) {
		_, _, key := newTokenPlanRegressionSubscription(t)
		channel := &model.Channel{Name: "review", Type: constant.ChannelTypeOpenAI, Key: "test", Status: common.ChannelStatusEnabled, Models: "review-denied", Group: "default"}
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "review-denied", ChannelId: channel.Id, Enabled: true}).Error)
		engine := gin.New()
		engine.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.TokenPlanGuard(), middleware.Distribute(), func(c *gin.Context) { c.Status(http.StatusOK) })
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"review-denied","messages":[{"role":"user","content":"hi"}]}`))
		r.Header.Set("Authorization", "Bearer "+key.Key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})
	t.Run("audio accounting without wallet", func(t *testing.T) {
		user, sub, key := newTokenPlanRegressionSubscription(t)
		record, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 100, false)
		require.NoError(t, err)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, UserId: user.Id, TokenId: key.Id, TokenPlanId: sub.Id, TokenPlanMetering: model.TokenPlanMeteringRaw, TokenPlanRequestId: record.RequestId, OriginModelName: "review-allowed", StartTime: time.Now()}
		usage := &dto.Usage{PromptTokens: 20, CompletionTokens: 30, TotalTokens: 50, PromptTokensDetails: dto.InputTokenDetails{AudioTokens: 20}, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 30}}
		service.PostAudioConsumeQuota(c, info, usage, nil)
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(50), updated.UsedTokens)
		assert.Equal(t, int64(50), updated.Window1Used)
		var wallet model.User
		require.NoError(t, model.DB.First(&wallet, user.Id).Error)
		assert.Equal(t, user.Quota, wallet.Quota)
	})
	t.Run("concurrent reservations and idempotent settlement", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		type result struct {
			record *model.TokenPlanPreConsumeRecord
			err    error
		}
		start, results := make(chan struct{}), make(chan result, 2)
		for range 2 {
			go func() {
				<-start
				record, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 60, false)
				results <- result{record, err}
			}()
		}
		close(start)
		var accepted *model.TokenPlanPreConsumeRecord
		for range 2 {
			r := <-results
			if r.err == nil {
				require.Nil(t, accepted)
				accepted = r.record
			} else {
				assert.ErrorIs(t, r.err, model.ErrTokenPlanQuotaExhausted)
			}
		}
		require.NotNil(t, accepted)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, accepted.RequestId, 30, false))
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, accepted.RequestId, 30, false))
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, accepted.RequestId, 0, true))
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(30), updated.UsedTokens)
	})
	t.Run("window rollover and reset refunds", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		first, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 40, false)
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(sub).Update("anchor_at", common.GetTimestamp()-sub.Window1Seconds).Error)
		second, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 20, false)
		require.NoError(t, err)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, first.RequestId, 10, false))
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(20), updated.Window1Used)
		_, err = model.ResetTokenPlanUsage(user.Id)
		require.NoError(t, err)
		third, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 25, false)
		require.NoError(t, err)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, second.RequestId, 0, true))
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, third.RequestId, 15, false))
		updated, err = model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(15), updated.UsedTokens)
		assert.Equal(t, int64(15), updated.Window1Used)
	})
	t.Run("unbounded output holds remaining budget", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		first, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 10, true)
		require.NoError(t, err)
		assert.Equal(t, int64(100), first.Tokens)
		_, err = model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 10, false)
		assert.ErrorIs(t, err, model.ErrTokenPlanQuotaExhausted)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, first.RequestId, 20, false))
		second, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 10, false)
		require.NoError(t, err)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, second.RequestId, 0, true))
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(20), updated.UsedTokens)
	})
	t.Run("text requires an explicit output bound", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		for _, request := range []dto.Request{&dto.GeneralOpenAIRequest{}, &dto.OpenAIResponsesRequest{}, &dto.ClaudeRequest{}, &dto.GeminiChatRequest{}} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenPlanId: sub.Id, Request: request, RelayMode: relayconstant.RelayModeChatCompletions}
			apiErr := service.PrepareTokenPlanBilling(c, info, 1, &types.TokenCountMeta{})
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Empty(t, info.TokenPlanRequestId)
		}
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Zero(t, updated.UsedTokens)
	})
	t.Run("settlement uses frozen model and cache rates", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		record, err := model.ReserveTokenPlanTokens(user.Id, sub.Id, common.GetUUID(), 60, false)
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{UserId: user.Id, TokenPlanId: sub.Id, TokenPlanMetering: model.TokenPlanMeteringWeighted, TokenPlanRequestId: record.RequestId, OriginModelName: "review-allowed"}
		info.TokenPlanRates.ModelRatio, info.TokenPlanRates.CacheRatio = 2, 0.5
		require.NoError(t, service.SettleTokenPlanUsage(info, model.TokenPlanUsageDetail{PromptTokens: 10, CacheTokens: 4, CompletionTokens: 10}))
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(44), updated.UsedTokens)
		var receipt model.TokenPlanPreConsumeRecord
		require.NoError(t, model.DB.Where("request_id = ?", record.RequestId).First(&receipt).Error)
		assert.Equal(t, int64(44), receipt.ConsumedTokens)
	})
	t.Run("concurrent first purchases share one subscription and key", func(t *testing.T) {
		_, template, _ := newTokenPlanRegressionSubscription(t)
		user := &model.User{Username: "first-" + common.GetUUID()[:12], AffCode: common.GetUUID()[:8], Status: common.UserStatusEnabled, Group: "default"}
		require.NoError(t, model.DB.Create(user).Error)
		start, results := make(chan struct{}), make(chan error, 2)
		for range 2 {
			go func() { <-start; _, _, err := model.PurchaseTokenPlan(user.Id, template.PlanId); results <- err }()
		}
		close(start)
		for range 2 {
			require.NoError(t, <-results)
		}
		var count int64
		require.NoError(t, model.DB.Model(&model.UserTokenPlan{}).Where("user_id = ? AND status = ?", user.Id, model.TokenPlanStatusActive).Count(&count).Error)
		assert.Equal(t, int64(1), count)
		require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ? AND token_plan_id IS NOT NULL", user.Id).Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})
	t.Run("malformed reset preserves usage", func(t *testing.T) {
		_, sub, _ := newTokenPlanRegressionSubscription(t)
		require.NoError(t, model.DB.Model(sub).Update("used_tokens", 40).Error)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/reset-usage", strings.NewReader(`{"user_id":"invalid"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		controller.AdminResetTokenPlanUsage(c)
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(40), updated.UsedTokens)
		assert.Contains(t, w.Body.String(), `"success":false`)
	})
	t.Run("renew upgrade and deleted key recovery", func(t *testing.T) {
		user, sub, key := newTokenPlanRegressionSubscription(t)
		_, _, err := model.PurchaseTokenPlan(user.Id, sub.PlanId)
		require.NoError(t, err)
		higher := &model.TokenPlan{Title: "Higher", PriceAmount: 159, TotalTokens: 150, DurationDays: 30, Enabled: true}
		higher.NormalizeDefaults()
		require.NoError(t, model.DB.Create(higher).Error)
		sub, _, err = model.PurchaseTokenPlan(user.Id, higher.Id)
		require.NoError(t, err)
		require.NoError(t, key.Delete())
		_, revived, err := model.PurchaseTokenPlan(user.Id, sub.PlanId)
		require.NoError(t, err)
		assert.Equal(t, key.Id, revived.Id)
		assert.NotEqual(t, key.Key, revived.Key)
		require.NoError(t, revived.Delete())
		reset, err := model.ResetTokenPlanKey(user.Id)
		require.NoError(t, err)
		assert.Equal(t, key.Id, reset.Id)
		assert.NotEqual(t, revived.Key, reset.Key)
	})
	t.Run("counting disabled still reserves and delays", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		constant.CountToken = false
		defer func() { constant.CountToken = true }()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenPlanId, sub.Id)
		common.SetContextKey(c, constant.ContextKeyTokenPlanMetering, model.TokenPlanMeteringRaw)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "review-allowed")
		common.SetContextKey(c, constant.ContextKeyTokenPlanThrottle, &model.TokenPlanThrottle{FirstByteDelayEnabled: true, FirstByteDelayPromptThreshold: 1, FirstByteDelayProbability: 100, FirstByteDelayMinMs: 250, FirstByteDelayMaxMs: 250})
		info := &relaycommon.RelayInfo{UserId: user.Id, OriginModelName: "review-allowed", RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, Request: &dto.GeneralOpenAIRequest{Model: "review-allowed", MaxTokens: common.GetPointer(uint(10)), Messages: []dto.Message{{Role: "user", Content: "hello"}}}}
		require.Nil(t, relay.PrepareRequestBilling(c, info))
		assert.Positive(t, info.GetEstimatePromptTokens())
		assert.Equal(t, 250*time.Millisecond, info.TakeFirstByteDelay())
		assert.Zero(t, info.TakeFirstByteDelay())
		require.NotEmpty(t, info.TokenPlanRequestId)
		apiErr := types.NewErrorWithStatusCode(fmt.Errorf("upstream failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
		relay.RefundFailedRequestBilling(c, info, apiErr)
		relay.RefundFailedRequestBilling(c, info, apiErr)
		updated, err := model.GetUserTokenPlanById(sub.Id)
		require.NoError(t, err)
		assert.Zero(t, updated.UsedTokens)
	})
	t.Run("websocket rechecks subscription after handshake", func(t *testing.T) {
		_, sub, key := newTokenPlanRegressionSubscription(t)
		engine := gin.New()
		SetRelayRouter(engine)
		server := httptest.NewServer(engine)
		defer server.Close()
		header := http.Header{"Authorization": []string{"Bearer " + key.Key}}
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", header)
		require.NoError(t, err)
		defer ws.Close()
		require.NoError(t, model.DB.Model(sub).Update("status", model.TokenPlanStatusCancelled).Error)
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"review-allowed","input":"hi"}`)))
		require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, body, err := ws.ReadMessage()
		require.NoError(t, err)
		assert.Contains(t, string(body), `"type":"error"`)
		assert.Contains(t, string(body), string(types.ErrorCodeInsufficientUserQuota))
	})
	t.Run("overflow never skips positive usage", func(t *testing.T) {
		got, clamp := model.WeightedTokensChecked(model.TokenPlanUsageDetail{PromptTokens: 1000, ModelRatio: math.MaxFloat64}, model.TokenPlanMeteringWeighted)
		assert.Equal(t, int64(common.MaxQuota), got)
		assert.NotNil(t, clamp)
		assert.Equal(t, int64(common.MaxQuota), model.WeightedTokens(model.TokenPlanUsageDetail{PromptTokens: math.MaxInt, CompletionTokens: math.MaxInt}, model.TokenPlanMeteringRaw))
	})
	t.Run("non-token endpoints reject before reservation", func(t *testing.T) {
		user, sub, _ := newTokenPlanRegressionSubscription(t)
		for _, mode := range []int{relayconstant.RelayModeAlphaSearch, relayconstant.RelayModeToolsSearch, relayconstant.RelayModeToolsFetch, relayconstant.RelayModeRealtime} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/search", nil)
			common.SetContextKey(c, constant.ContextKeyTokenPlanId, sub.Id)
			info := &relaycommon.RelayInfo{UserId: user.Id, RelayMode: mode}
			apiErr := relay.PrepareRequestBilling(c, info)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			assert.Empty(t, info.TokenPlanRequestId)
		}
	})
	t.Run("purchase checks groups and preserves key reveal scope", func(t *testing.T) {
		user, sub, key := newTokenPlanRegressionSubscription(t)
		for _, group := range []string{"", "not-an-accessible-group"} {
			require.NoError(t, model.DB.Model(&model.TokenPlan{}).Where("id = ?", sub.PlanId).Update("group_limits", group).Error)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("id", user.Id)
			c.Set("use_access_token", true)
			c.Set("access_token_scopes", []string{"wallet:write"})
			c.Request = httptest.NewRequest(http.MethodPost, "/purchase", strings.NewReader(fmt.Sprintf(`{"plan_id":%d}`, sub.PlanId)))
			c.Request.Header.Set("Content-Type", "application/json")
			controller.PurchaseTokenPlan(c)
			assert.NotContains(t, w.Body.String(), key.Key)
			if group == "" {
				assert.Contains(t, w.Body.String(), `"success":true`)
			} else {
				assert.Contains(t, w.Body.String(), `"success":false`)
			}
		}
	})
}
