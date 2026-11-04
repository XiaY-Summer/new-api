package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func TokenPlanEpayNotify(c *gin.Context) {
	status := completeTokenPlanEpay(c)
	if status != "success" {
		status = "fail"
	}
	c.String(http.StatusOK, status)
}

func TokenPlanEpayReturn(c *gin.Context) {
	status := completeTokenPlanEpay(c)
	c.Redirect(http.StatusFound, paymentReturnPath("/token-plan?pay="+status))
}

// completeTokenPlanEpay is shared by the server notification and browser return.
// Callbacks remain available for existing orders if new payments are disabled.
func completeTokenPlanEpay(c *gin.Context) string {
	values := c.Request.URL.Query()
	if c.Request.Method == http.MethodPost {
		if err := c.Request.ParseForm(); err != nil {
			return "fail"
		}
		values = c.Request.PostForm
	}
	params := make(map[string]string, len(values))
	for key, entries := range values {
		if len(entries) != 1 {
			return "fail"
		}
		params[key] = entries[0]
	}
	client := GetEpayClient()
	if client == nil || params["pid"] != client.Config.PartnerID {
		return "fail"
	}
	verified, err := client.Verify(params)
	if err != nil || !verified.VerifyStatus {
		return "fail"
	}
	if verified.TradeStatus != epay.StatusTradeSuccess {
		return "pending"
	}
	if err := model.CompleteTokenPlanOrder(verified.ServiceTradeNo, verified.Money, verified.Type, common.GetJsonString(verified)); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("TOKEN PLAN EPay completion failed for %s: %v", verified.ServiceTradeNo, err))
		if errors.Is(err, model.ErrTokenPlanFulfillmentPending) {
			return "processing"
		}
		return "fail"
	}
	return "success"
}

// ListTokenPlanPaymentIssues exposes only the caller's paid, unfulfilled orders.
func ListTokenPlanPaymentIssues(c *gin.Context) {
	if c.GetInt("id") <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	listTokenPlanPaymentIssues(c, c.GetInt("id"))
}

func AdminListTokenPlanPaymentIssues(c *gin.Context) {
	listTokenPlanPaymentIssues(c, 0)
}

func listTokenPlanPaymentIssues(c *gin.Context, userId int) {
	page := common.GetPageQuery(c)
	page.PageSize = max(1, page.PageSize)
	orders, total, err := model.ListPaidPendingTokenPlanOrders(userId, page.GetStartIdx(), page.PageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if userId > 0 {
		for _, order := range orders {
			order.FulfillmentError = ""
		}
	}
	page.SetTotal(int(total))
	page.SetItems(orders)
	common.ApiSuccess(c, page)
}

func AdminRetryTokenPlanOrder(c *gin.Context) {
	if err := model.FulfillTokenPlanOrder(c.Param("trade_no")); err != nil {
		common.ApiErrorT(c, "TOKEN PLAN activation could not be completed. Check the payment issue details.")
		return
	}
	common.ApiSuccess(c, nil)
}
