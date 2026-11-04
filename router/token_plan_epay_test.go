package router

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureTokenPlanEpay(t *testing.T) {
	t.Helper()
	previousPayment := *operation_setting.GetPaymentSetting()
	address, id, key, methods := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods
	callback, server := operation_setting.CustomCallbackAddress, system_setting.ServerAddress
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = previousPayment
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayMethods = address, id, key, methods
		operation_setting.CustomCallbackAddress, system_setting.ServerAddress = callback, server
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://epay.example", "1001", "test-token-plan-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay", "name": "Alipay"}, {"type": "wxpay", "name": "WeChat"}}
	operation_setting.CustomCallbackAddress, system_setting.ServerAddress = "https://callback.example", "https://console.example"
}

func newTokenPlanEpayFixture(t *testing.T) (*model.User, *model.TokenPlan) {
	t.Helper()
	user := &model.User{Username: "epay-" + common.GetUUID()[:12], AffCode: common.GetUUID()[:8], Status: common.UserStatusEnabled, Group: "default", Quota: 1000}
	plan := &model.TokenPlan{Title: "EPay plan", Enabled: true, PriceAmount: 59, Currency: "CNY", DurationDays: 30, TotalTokens: 100, MeteringMode: model.TokenPlanMeteringRaw}
	plan.NormalizeDefaults()
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(plan).Error)
	return user, plan
}

func tokenPlanCheckout(t *testing.T, userId, planId int, method string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", userId)
	c.Set("use_access_token", true)
	c.Set("access_token_scopes", []string{"wallet:write"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/token-plan/purchase", strings.NewReader(fmt.Sprintf(`{"plan_id":%d,"payment_method":%q,"money":0.01}`, planId, method)))
	c.Request.Header.Set("Content-Type", "application/json")
	controller.PurchaseTokenPlan(c)
	return w
}

func tokenPlanCallback(t *testing.T, method string, params map[string]string, browserReturn bool) *httptest.ResponseRecorder {
	t.Helper()
	signed, err := epay.GenerateParamsWithError(params, operation_setting.EpayKey)
	require.NoError(t, err)
	values := url.Values{}
	for key, value := range signed {
		values.Set(key, value)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/token-plan/epay/notify?"+values.Encode(), nil)
	if method == http.MethodPost {
		c.Request = httptest.NewRequest(method, "/api/token-plan/epay/notify", strings.NewReader(values.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if browserReturn {
		controller.TokenPlanEpayReturn(c)
	} else {
		controller.TokenPlanEpayNotify(c)
	}
	return w
}

func TestTokenPlanEpayPayment(t *testing.T) {
	for _, dialect := range []struct{ name, env string }{{"sqlite", ""}, {"mysql", "TOKEN_PLAN_TEST_MYSQL_DSN"}, {"postgres", "TOKEN_PLAN_TEST_POSTGRES_DSN"}} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect.name, upgrade), func(t *testing.T) {
				if dialect.env != "" && os.Getenv(dialect.env) == "" {
					t.Skip(dialect.env + " is not configured")
				}
				setupTokenPlanRegressionDB(t, os.Getenv(dialect.env), upgrade)
				configureTokenPlanEpay(t)
				runTokenPlanEpayCases(t)
			})
		}
	}
}

func runTokenPlanEpayCases(t *testing.T) {
	runTokenPlanPaymentFixCases(t)
	t.Run("checkout does not grant and signed callbacks grant once", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		w := tokenPlanCheckout(t, user.Id, plan.Id, "alipay")
		var checkout struct {
			Success bool              `json:"success"`
			URL     string            `json:"url"`
			Data    map[string]string `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &checkout))
		require.True(t, checkout.Success, w.Body.String())
		assert.Equal(t, "https://epay.example/submit.php", checkout.URL)
		assert.Equal(t, "59.00", checkout.Data["money"])
		assert.Equal(t, "https://callback.example/api/token-plan/epay/notify", checkout.Data["notify_url"])
		assert.Equal(t, "https://callback.example/api/token-plan/epay/return", checkout.Data["return_url"])
		sub, err := model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		require.Nil(t, sub)
		params := map[string]string{"pid": "1001", "out_trade_no": checkout.Data["out_trade_no"], "trade_no": "provider-1", "type": "alipay", "money": "59.00", "trade_status": epay.StatusTradeSuccess}
		for _, field := range []string{"money", "pid", "type", "out_trade_no"} {
			t.Run("reject wrong "+field, func(t *testing.T) {
				invalid := maps.Clone(params)
				invalid[field] = "0.01"
				assert.Equal(t, "fail", tokenPlanCallback(t, http.MethodGet, invalid, false).Body.String())
			})
		}
		unsigned := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(unsigned)
		c.Request = httptest.NewRequest(http.MethodGet, "/notify?pid=1001&trade_status=TRADE_SUCCESS&out_trade_no="+params["out_trade_no"], nil)
		controller.TokenPlanEpayNotify(c)
		assert.Equal(t, "fail", unsigned.Body.String())
		signed, err := epay.GenerateParamsWithError(params, operation_setting.EpayKey)
		require.NoError(t, err)
		signed["sign"] = "invalid-signature"
		values := url.Values{}
		for field, value := range signed {
			values.Set(field, value)
		}
		invalidSignature := httptest.NewRecorder()
		c, _ = gin.CreateTestContext(invalidSignature)
		c.Request = httptest.NewRequest(http.MethodGet, "/notify?"+values.Encode(), nil)
		controller.TokenPlanEpayNotify(c)
		assert.Equal(t, "fail", invalidSignature.Body.String())
		sub, err = model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		require.Nil(t, sub)
		pending := maps.Clone(params)
		pending["trade_status"] = "WAIT_BUYER_PAY"
		assert.Equal(t, "https://console.example/token-plan?pay=pending", tokenPlanCallback(t, http.MethodGet, pending, true).Header().Get("Location"))
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
		assert.Equal(t, "success", tokenPlanCallback(t, http.MethodPost, params, false).Body.String())
		operation_setting.GetPaymentSetting().ComplianceConfirmed = true
		assert.Equal(t, "https://console.example/token-plan?pay=success", tokenPlanCallback(t, http.MethodGet, params, true).Header().Get("Location"))
		assert.Equal(t, "https://console.example/token-plan?pay=success", tokenPlanCallback(t, http.MethodPost, params, true).Header().Get("Location"))
		assert.Equal(t, "success", tokenPlanCallback(t, http.MethodGet, params, false).Body.String())
		sub, err = model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		require.NotNil(t, sub)
		assert.Equal(t, plan.TotalTokens, sub.TotalTokens)
		var receipt model.TopUp
		require.NoError(t, model.DB.Where("trade_no = ?", params["out_trade_no"]).First(&receipt).Error)
		assert.Equal(t, int64(0), receipt.Amount)
		assert.Equal(t, 59.0, receipt.Money)
		assert.Equal(t, model.PaymentProviderEpay, receipt.PaymentProvider)
		require.NoError(t, model.DB.First(user, user.Id).Error)
		assert.Equal(t, 1000, user.Quota)
		var count int64
		require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})

	t.Run("repeat migrations preserve pending order snapshot and trade uniqueness", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		order, _, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "migration-"+common.GetUUID(), "alipay")
		require.NoError(t, err)
		for range 2 {
			require.NoError(t, model.DB.AutoMigrate(&model.TokenPlanOrder{}))
		}
		var reloaded model.TokenPlanOrder
		require.NoError(t, model.DB.First(&reloaded, order.Id).Error)
		assert.Equal(t, order.PlanSnapshot, reloaded.PlanSnapshot)
		assert.Equal(t, order.Money, reloaded.Money)
		assert.Equal(t, common.TopUpStatusPending, reloaded.Status)
		duplicate := *order
		duplicate.Id = 0
		require.Error(t, model.DB.Create(&duplicate).Error)
		require.NoError(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", ""))
	})

	t.Run("paid order preserves original terms after edit and unpublish", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		order, _, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "snapshot-"+common.GetUUID(), "alipay")
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(plan).Updates(map[string]any{"enabled": false, "price_amount": 999, "total_tokens": 9999, "duration_days": 1, "title": "edited"}).Error)
		require.NoError(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", ""))
		sub, err := model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		require.NotNil(t, sub)
		assert.Equal(t, int64(100), sub.TotalTokens)
		assert.Equal(t, int64(30*86400), sub.EndTime-sub.StartTime)
		assert.Equal(t, "EPay plan", sub.PlanTitle)
	})

	t.Run("concurrent renewal callbacks preserve usage and grant once per order", func(t *testing.T) {
		user, sub, key := newTokenPlanRegressionSubscription(t)
		require.NoError(t, model.DB.Model(sub).Updates(map[string]any{"used_tokens": 7, "anchor_at": 1234, "window1_used": 7}).Error)
		orders := make([]*model.TokenPlanOrder, 2)
		for i := range orders {
			order, _, err := model.CreateTokenPlanOrder(user.Id, sub.PlanId, "renew-"+common.GetUUID(), "alipay")
			require.NoError(t, err)
			orders[i] = order
		}
		results := make(chan error, 8)
		for i := range 8 {
			go func() { results <- model.CompleteTokenPlanOrder(orders[i%2].TradeNo, "59", "alipay", "") }()
		}
		for range 8 {
			require.NoError(t, <-results)
		}
		updated, err := model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(300), updated.TotalTokens)
		assert.Equal(t, int64(7), updated.UsedTokens)
		assert.Equal(t, int64(1234), updated.AnchorAt)
		assert.Equal(t, int64(7), updated.Window1Used)
		assert.Equal(t, sub.EndTime+2*30*86400, updated.EndTime)
		assert.Equal(t, key.Id, updated.TokenId)
	})

	t.Run("receipt failure rolls back grant and allows retry", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		order, _, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "rollback-"+common.GetUUID(), "alipay")
		require.NoError(t, err)
		receipt := &model.TopUp{UserId: user.Id, TradeNo: order.TradeNo, Status: common.TopUpStatusPending}
		require.NoError(t, model.DB.Create(receipt).Error)
		require.Error(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", ""))
		sub, err := model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		assert.Nil(t, sub)
		require.NoError(t, model.DB.First(order, order.Id).Error)
		assert.Equal(t, model.TokenPlanOrderPaidPending, order.Status)
		assert.Positive(t, order.PaidTime)
		assert.NotEmpty(t, order.FulfillmentError)
		require.NoError(t, model.DB.Delete(receipt).Error)
		require.NoError(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", ""))
	})

	t.Run("checkout rejects missing payment method and disabled gateway", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		for _, method := range []string{"", "stripe", "not-configured"} {
			assert.Contains(t, tokenPlanCheckout(t, user.Id, plan.Id, method).Body.String(), `"success":false`)
		}
		previousKey := operation_setting.EpayKey
		operation_setting.EpayKey = ""
		assert.Contains(t, tokenPlanCheckout(t, user.Id, plan.Id, "alipay").Body.String(), `"success":false`)
		operation_setting.EpayKey = previousKey
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
		assert.Contains(t, tokenPlanCheckout(t, user.Id, plan.Id, "alipay").Body.String(), `"success":false`)
		operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	})

	t.Run("checkout rejects unsupported currency fractional price and downgrade", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		for _, update := range []map[string]any{{"currency": "USD"}, {"currency": "CNY", "price_amount": 0.001}, {"price_amount": 59.005}, {"price_amount": 59, "enabled": false}} {
			require.NoError(t, model.DB.Model(plan).Updates(update).Error)
			_, _, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "reject-"+common.GetUUID(), "alipay")
			require.Error(t, err)
		}
		require.NoError(t, model.DB.Model(plan).Updates(map[string]any{"enabled": true, "total_tokens": 1000}).Error)
		_, _, err := model.PurchaseTokenPlan(user.Id, plan.Id)
		require.NoError(t, err)
		lower := &model.TokenPlan{Title: "lower", Enabled: true, PriceAmount: 1, Currency: "CNY", DurationDays: 30, TotalTokens: 1}
		lower.NormalizeDefaults()
		require.NoError(t, model.DB.Create(lower).Error)
		_, _, err = model.CreateTokenPlanOrder(user.Id, lower.Id, "lower-"+common.GetUUID(), "alipay")
		require.Error(t, err)
	})
}

func runTokenPlanPaymentFixCases(t *testing.T) {
	t.Run("payment issue routes enforce role and scoped authorization", func(t *testing.T) {
		engine := gin.New()
		SetApiRouter(engine)
		for _, tc := range []struct {
			name, method, path string
			role               int
			scopes             []string
			status             int
		}{
			{"anonymous", http.MethodGet, "/api/token-plan/payment-issues", 0, nil, http.StatusUnauthorized},
			{"wallet read", http.MethodGet, "/api/token-plan/payment-issues", common.RoleCommonUser, []string{"wallet:read"}, http.StatusOK},
			{"missing wallet read", http.MethodGet, "/api/token-plan/payment-issues", common.RoleCommonUser, []string{"profile:read"}, http.StatusForbidden},
			{"user cannot read all orders", http.MethodGet, "/api/token-plan/admin/payment-issues", common.RoleCommonUser, []string{"billing:read"}, http.StatusForbidden},
			{"admin read", http.MethodGet, "/api/token-plan/admin/payment-issues", common.RoleAdminUser, []string{"billing:read"}, http.StatusOK},
			{"admin missing read", http.MethodGet, "/api/token-plan/admin/payment-issues", common.RoleAdminUser, []string{"wallet:read"}, http.StatusForbidden},
			{"user cannot retry", http.MethodPost, "/api/token-plan/admin/orders/unpaid/retry", common.RoleCommonUser, []string{"billing:write"}, http.StatusForbidden},
			{"read token cannot retry", http.MethodPost, "/api/token-plan/admin/orders/unpaid/retry", common.RoleAdminUser, []string{"billing:read"}, http.StatusForbidden},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.role > 0 {
					user, _ := newTokenPlanEpayFixture(t)
					require.NoError(t, model.DB.Model(user).Update("role", tc.role).Error)
					suffix, err := common.GenerateRandomCharsKey(43)
					require.NoError(t, err)
					raw := model.AccessTokenPrefix + suffix
					token := &model.UserAccessToken{Name: "payment route regression", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
					require.NoError(t, token.SetScopes(tc.scopes))
					require.NoError(t, model.CreateUserAccessToken(user.Id, token, service.AccessTokenMaxPerUser))
					r.Header.Set("Authorization", "Bearer "+raw)
				}
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, r)
				assert.Equal(t, tc.status, w.Code, w.Body.String())
			})
		}
	})
	t.Run("paid fulfillment failures remain visible and can be retried", func(t *testing.T) {
		for _, scenario := range []string{"another plan paid first", "template deleted", "user disabled"} {
			t.Run(scenario, func(t *testing.T) {
				user, plan := newTokenPlanEpayFixture(t)
				order, _, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "paid-issue-"+common.GetUUID(), "alipay")
				require.NoError(t, err)
				// Manual retries must never grant an unpaid order.
				require.Error(t, model.FulfillTokenPlanOrder(order.TradeNo))
				switch scenario {
				case "another plan paid first":
					higher := *plan
					higher.Id, higher.TotalTokens = 0, 200
					require.NoError(t, model.DB.Create(&higher).Error)
					_, _, err := model.PurchaseTokenPlan(user.Id, higher.Id)
					require.NoError(t, err)
				case "template deleted":
					_, err := model.DeleteTokenPlan(plan.Id)
					require.NoError(t, err)
				case "user disabled":
					require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusDisabled).Error)
				}
				params := map[string]string{"pid": "1001", "out_trade_no": order.TradeNo, "trade_no": "provider-issue", "type": "alipay", "money": "59.00", "trade_status": epay.StatusTradeSuccess}
				assert.Equal(t, "fail", tokenPlanCallback(t, http.MethodPost, params, false).Body.String())
				assert.Equal(t, "https://console.example/token-plan?pay=processing", tokenPlanCallback(t, http.MethodGet, params, true).Header().Get("Location"))
				require.NoError(t, model.DB.First(order, order.Id).Error)
				assert.Equal(t, model.TokenPlanOrderPaidPending, order.Status)
				assert.Positive(t, order.PaidTime)
				assert.NotEmpty(t, order.FulfillmentError)
				assert.Contains(t, order.ProviderPayload, "provider-issue")
				payload, paidTime := order.ProviderPayload, order.PaidTime
				require.Error(t, model.CompleteTokenPlanOrder(order.TradeNo, "0.01", "alipay", "tampered"))
				require.NoError(t, model.DB.First(order, order.Id).Error)
				assert.Equal(t, payload, order.ProviderPayload)
				assert.Equal(t, paidTime, order.PaidTime)
				other, _ := newTokenPlanEpayFixture(t)
				for _, viewer := range []*model.User{user, other} {
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Set("id", viewer.Id)
					c.Request = httptest.NewRequest(http.MethodGet, "/api/token-plan/payment-issues?user_id="+fmt.Sprint(user.Id), nil)
					controller.ListTokenPlanPaymentIssues(c)
					assert.NotContains(t, w.Body.String(), payload)
					assert.NotContains(t, w.Body.String(), order.FulfillmentError)
					if viewer.Id == user.Id {
						assert.Contains(t, w.Body.String(), order.TradeNo)
					} else {
						assert.NotContains(t, w.Body.String(), order.TradeNo)
					}
				}
				switch scenario {
				case "another plan paid first":
					require.NoError(t, model.DB.Model(&model.UserTokenPlan{}).Where("user_id = ?", user.Id).Update("end_time", common.GetTimestamp()-1).Error)
				case "template deleted":
					require.NoError(t, model.DB.Create(plan).Error)
				case "user disabled":
					require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusEnabled).Error)
				}
				// Administrator retry races with a duplicate provider notification.
				results := make(chan error, 2)
				go func() { results <- model.FulfillTokenPlanOrder(order.TradeNo) }()
				go func() { results <- model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", "duplicate") }()
				require.NoError(t, <-results)
				require.NoError(t, <-results)
				require.NoError(t, model.DB.First(order, order.Id).Error)
				assert.Equal(t, common.TopUpStatusSuccess, order.Status)
				assert.Empty(t, order.FulfillmentError)
				assert.Equal(t, payload, order.ProviderPayload)
				sub, err := model.GetActiveUserTokenPlan(user.Id)
				require.NoError(t, err)
				require.NotNil(t, sub)
				assert.Equal(t, int64(100), sub.TotalTokens)
				var receipts int64
				require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", order.TradeNo).Count(&receipts).Error)
				assert.Equal(t, int64(1), receipts)
			})
		}
	})

	t.Run("renewal applies snapshot and preserves in flight settlement", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		require.NoError(t, model.DB.Model(plan).Updates(map[string]any{"window1_seconds": 100, "window2_seconds": 1000, "window1_tokens": 20, "window2_tokens": 60, "model_limits": "old-model"}).Error)
		sub, _, err := model.PurchaseTokenPlan(user.Id, plan.Id)
		require.NoError(t, err)
		now := common.GetTimestamp()
		require.NoError(t, model.DB.Model(sub).Updates(map[string]any{"anchor_at": now - 50, "used_tokens": 7, "window1_used": 7, "window2_used": 7}).Error)
		oldRequest := "old-window-" + common.GetUUID()
		_, err = model.ReserveTokenPlanTokens(user.Id, sub.Id, oldRequest, 5, false)
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(sub).Updates(map[string]any{"anchor_at": now - 150, "window1_index": 1, "window1_used": 0}).Error)
		currentRequest := "current-window-" + common.GetUUID()
		_, err = model.ReserveTokenPlanTokens(user.Id, sub.Id, currentRequest, 3, false)
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(plan).Updates(map[string]any{"title": "renewed", "window1_seconds": 50, "window2_seconds": 500, "window1_tokens": 50, "window2_tokens": 90, "model_limits": "new-model", "group_limits": "default"}).Error)
		order, snapshot, err := model.CreateTokenPlanOrder(user.Id, plan.Id, "snapshot-renew-"+common.GetUUID(), "alipay")
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(plan).Update("window1_tokens", 999).Error)
		require.NoError(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", "renewal"))
		updated, err := model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, int64(200), updated.TotalTokens)
		assert.Equal(t, int64(15), updated.UsedTokens)
		assert.Equal(t, snapshot.Window1Tokens, updated.Window1Tokens)
		assert.Equal(t, snapshot.Window2Tokens, updated.Window2Tokens)
		assert.Equal(t, snapshot.Window1Seconds, updated.Window1Seconds)
		assert.Equal(t, snapshot.Window2Seconds, updated.Window2Seconds)
		assert.Equal(t, snapshot.ModelLimits, updated.ModelLimits)
		assert.Equal(t, snapshot.GroupLimits, updated.GroupLimits)
		assert.Equal(t, snapshot.Title, updated.PlanTitle)
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, oldRequest, 0, true))
		require.NoError(t, model.SettleTokenPlanTokens(user.Id, sub.Id, currentRequest, 1, false))
		updated, err = model.GetActiveUserTokenPlan(user.Id)
		require.NoError(t, err)
		assert.Equal(t, int64(8), updated.UsedTokens)
		assert.Equal(t, int64(1), updated.Window1Used)
		assert.Equal(t, int64(8), updated.Window2Used)
	})

	t.Run("ampersand names and failed signing", func(t *testing.T) {
		user, plan := newTokenPlanEpayFixture(t)
		require.NoError(t, model.DB.Model(plan).Update("title", "Research & Coding").Error)
		var checkout struct {
			Success bool
			Data    map[string]string
		}
		w := tokenPlanCheckout(t, user.Id, plan.Id, "alipay")
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &checkout))
		require.True(t, checkout.Success, w.Body.String())
		assert.Equal(t, "TOKEN PLAN: Research ＆ Coding", checkout.Data["name"])
		verified, err := controller.GetEpayClient().Verify(checkout.Data)
		require.NoError(t, err)
		assert.True(t, verified.VerifyStatus)
		previous := operation_setting.CustomCallbackAddress
		operation_setting.CustomCallbackAddress = "https://callback.example?x=1&y=2"
		w = tokenPlanCheckout(t, user.Id, plan.Id, "alipay")
		operation_setting.CustomCallbackAddress = previous
		assert.Contains(t, w.Body.String(), `"success":false`)
		var latest model.TokenPlanOrder
		require.NoError(t, model.DB.Where("user_id = ?", user.Id).Order("id desc").First(&latest).Error)
		assert.Equal(t, common.TopUpStatusExpired, latest.Status)
		require.Error(t, model.FulfillTokenPlanOrder(latest.TradeNo))
	})

	t.Run("uncapped quota is highest tier and survives renewal", func(t *testing.T) {
		user, finite := newTokenPlanEpayFixture(t)
		_, _, err := model.PurchaseTokenPlan(user.Id, finite.Id)
		require.NoError(t, err)
		uncapped := *finite
		uncapped.Id, uncapped.TotalTokens = 0, 0
		require.NoError(t, model.DB.Create(&uncapped).Error)
		for range 2 {
			order, _, err := model.CreateTokenPlanOrder(user.Id, uncapped.Id, "uncapped-"+common.GetUUID(), "alipay")
			require.NoError(t, err)
			require.NoError(t, model.CompleteTokenPlanOrder(order.TradeNo, "59", "alipay", "uncapped"))
			sub, err := model.GetActiveUserTokenPlan(user.Id)
			require.NoError(t, err)
			assert.Zero(t, sub.TotalTokens)
		}
		_, _, err = model.CreateTokenPlanOrder(user.Id, finite.Id, "downgrade-"+common.GetUUID(), "alipay")
		require.Error(t, err)
	})
}
