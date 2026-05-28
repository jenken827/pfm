package utils

import (
	"fmt"
	"runtime/debug"

	"jenken827.com/jenken827/pfm/internal/llog"
)

// Go 安全启动 goroutine，自动捕获 panic 并打印堆栈
func Go(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				llog.L().Errorf("[GOROUTINE PANIC] %v", err)
				llog.L().Error(string(debug.Stack()))
			}
		}()
		fn()
	}()
}

// GoWithError 安全启动 goroutine，捕获 panic 并通过 error channel 返回
func GoWithError(fn func() error, errCh chan<- error) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				stack := string(debug.Stack())
				llog.L().Errorf("[GOROUTINE PANIC] %v\n%s", err, stack)
				if errCh != nil {
					errCh <- fmt.Errorf("panic: %v", err)
				}
			}
		}()
		if err := fn(); err != nil && errCh != nil {
			errCh <- err
		}
	}()
}
