package common

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// FirstByteDelayGate 保证「一次客户端请求最多延迟一次首字节」。
//
// 决策发生在 relay.PrepareRequestBilling（prompt token 估算刚就绪，且在
// controller.Relay 的重试循环之外），消费发生在真正向客户端写第一个字节的位置：
//   - 流式：relay/helper.StreamScannerHandler 的首包入队前
//   - 非流式：service.IOCopyBytesGracefully 写回前
//
// 用 sync.Once 而不是 bool：重试路径可能重复进入写回点，但只应睡一次。
// 若第一次 attempt 在写出任何字节前就失败，gate 未被 Take，第二次 attempt 会
// 拿到同一个时长并延迟一次——客户端确实还没收到东西，这个行为是对的。
type FirstByteDelayGate struct {
	delay time.Duration
	once  sync.Once
}

// NewFirstByteDelayGate 创建闸门；d <= 0 返回 nil，调用方无需判空。
func NewFirstByteDelayGate(d time.Duration) *FirstByteDelayGate {
	if d <= 0 {
		return nil
	}
	return &FirstByteDelayGate{delay: d}
}

// Take 返回本次应延迟的时长，只返回一次；后续调用返回 0。
func (g *FirstByteDelayGate) Take() time.Duration {
	if g == nil {
		return 0
	}
	var taken time.Duration
	g.once.Do(func() { taken = g.delay })
	return taken
}

// SetFirstByteDelayGate 把闸门放进 gin context，供拿不到 RelayInfo 的写回路径读取。
func SetFirstByteDelayGate(c *gin.Context, g *FirstByteDelayGate) {
	if c == nil || g == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyTokenPlanFirstByteDelay, g)
}

// TakeFirstByteDelayFromContext 从 gin context 取走闸门并消耗它。
func TakeFirstByteDelayFromContext(c *gin.Context) time.Duration {
	if c == nil {
		return 0
	}
	g, ok := common.GetContextKeyType[*FirstByteDelayGate](c, constant.ContextKeyTokenPlanFirstByteDelay)
	if !ok {
		return 0
	}
	return g.Take()
}

// WaitFirstByteDelay 在 d 内等待。
//
// stop 为 nil 时该分支永久阻塞（非流式路径不需要停止信号）；流式路径必须传
// stopChan，否则客户端断开后 scanner 协程会卡在 sleep 里，让 cleanup 的
// wg.Wait() 永久阻塞。返回 false 表示被取消，调用方应立即返回。
func WaitFirstByteDelay(ctx context.Context, stop <-chan bool, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-stop:
		return false
	}
}
