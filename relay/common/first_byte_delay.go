package common

import (
	"context"
	"sync"
	"time"
)

// FirstByteDelayGate 保证「一次客户端请求最多延迟一次」。
//
// 决策发生在 relay.PrepareRequestBilling（prompt token 估算刚就绪，且在
// controller.Relay 的重试循环之外），消费发生在 controller.Relay 里、
// 真正转发给上游之前——等待期间上游完全不被占用，也不会因为不读响应体
// 而产生 TCP 背压。
//
// 用 sync.Once 而不是 bool：重试路径会再次走到消费点，但只应睡一次。
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

// WaitFirstByteDelay 在 d 内等待，客户端断开时立即返回 false。
//
// 这里不接 stop 通道：调用点在转发上游之前，此时还没有流可以停止，
// 唯一需要响应的取消信号就是请求本身的 context。
func WaitFirstByteDelay(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
