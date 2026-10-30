package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jenken827/pfm/internal/auth"
)

// principalContextKey ctx 里放令牌身份用的私有键
type principalContextKey struct{}

// WithPrincipal 把令牌身份放进 ctx（由 server 层鉴权后注入）
func WithPrincipal(ctx context.Context, principal *auth.McpPrincipal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFrom 取出令牌身份；没有（理论上不会）返回 nil
func PrincipalFrom(ctx context.Context) *auth.McpPrincipal {
	principal, _ := ctx.Value(principalContextKey{}).(*auth.McpPrincipal)
	return principal
}

// ===== legacy 会话 =====

// legacySessionTTL 旧版会话的空闲回收时间（会话只存版本与客户端能力，不存业务状态）
const legacySessionTTL = 30 * time.Minute

// legacySession 旧版协议（initialize 握手）的会话
type legacySession struct {
	ID         string
	Version    string
	ClientName string
	lastSeen   time.Time
}

// sessionStore 旧版会话表（内存，进程内有效；重启后客户端需重新 initialize）
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*legacySession
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]*legacySession)}
}

// Create 新建会话（顺手回收过期项，不另起后台协程）
func (s *sessionStore) Create(version, clientName string) *legacySession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.lastSeen) > legacySessionTTL {
			delete(s.sessions, id)
		}
	}
	session := &legacySession{
		ID:         newSessionID(),
		Version:    version,
		ClientName: clientName,
		lastSeen:   now,
	}
	s.sessions[session.ID] = session
	return session
}

// Get 取会话并刷新活跃时间；不存在或已过期返回 nil
func (s *sessionStore) Get(id string) *legacySession {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil
	}
	now := time.Now()
	if now.Sub(session.lastSeen) > legacySessionTTL {
		delete(s.sessions, id)
		return nil
	}
	session.lastSeen = now
	return session
}

// Delete 结束会话（客户端发 DELETE 时调用）
func (s *sessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// newSessionID 生成会话 id（128 位随机，十六进制）
func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 随机数失败极罕见；退化成时间戳也能工作（会话不承载安全语义：
		// 真正的鉴权在 Bearer 令牌上）
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

// ===== 每令牌配额 =====

// tokenLimiter 每个令牌每分钟的调用配额（内存滑动窗口，够用即可）
type tokenLimiter struct {
	mu      sync.Mutex
	windows map[uint32]*limitWindow
}

type limitWindow struct {
	start time.Time
	count int
}

func newTokenLimiter() *tokenLimiter {
	return &tokenLimiter{windows: make(map[uint32]*limitWindow)}
}

// Allow 记一次调用并判断是否超配额；perMinute <= 0 表示不限制
func (l *tokenLimiter) Allow(tokenID uint32, perMinute int) bool {
	if perMinute <= 0 || tokenID == 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	window, ok := l.windows[tokenID]
	if !ok || now.Sub(window.start) >= time.Minute {
		// 进入新窗口时顺带清掉其它令牌的旧窗口，避免 map 无限增长
		if len(l.windows) > 1024 {
			for id, item := range l.windows {
				if now.Sub(item.start) >= 2*time.Minute {
					delete(l.windows, id)
				}
			}
		}
		l.windows[tokenID] = &limitWindow{start: now, count: 1}
		return true
	}
	if window.count >= perMinute {
		return false
	}
	window.count++
	return true
}
