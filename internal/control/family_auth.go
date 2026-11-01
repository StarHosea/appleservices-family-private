package control

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// FamilyAuthentication contains only local challenge routing and UI state.
// Apple authentication payloads remain in the family helper's process memory.
type FamilyAuthentication struct {
	RequestID string    `json:"request_id"`
	Organizer string    `json:"organizer"`
	Stage     string    `json:"stage"`
	ExpiresAt time.Time `json:"expires_at"`
}

type FamilyAuthenticationInput struct {
	Action string `json:"action"`
	Code   string `json:"code,omitempty"`
}

func (s *Server) BeginFamilyAuthentication(organizer string, expires time.Time) (string, <-chan FamilyAuthenticationInput, func()) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return "", nil, func() {}
	}
	id := hex.EncodeToString(key)
	s.mu.Lock()
	s.familyAuth = &FamilyAuthentication{RequestID: id, Organizer: organizer, Stage: "authenticating", ExpiresAt: expires}
	input := make(chan FamilyAuthenticationInput, 1)
	s.familyInput = input
	s.mu.Unlock()
	return id, input, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.familyAuth != nil && s.familyAuth.RequestID == id {
			s.familyAuth = nil
			s.familyInput = nil
		}
	}
}

func (s *Server) SetFamilyAuthenticationStage(id, stage string) {
	if stage != "repair-consent" && stage != "sms-code" && stage != "verifying-membership" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.familyAuth == nil || s.familyAuth.RequestID != id {
		return
	}
	state := *s.familyAuth
	state.Stage = stage
	s.familyAuth = &state
}

// Route challenge input outside the main worker command loop: that loop is
// waiting for this very helper. Never enqueue input on the ordinary action queue.
func (s *Server) familyAuthenticationAction(action Action) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.familyAuth
	if pending == nil || action.RequestID == "" || action.RequestID != pending.RequestID || !time.Now().Before(pending.ExpiresAt) {
		return Result{Stage: "family-auth-expired", Message: "家庭验证已过期，请重新读取家庭状态后再加入"}
	}
	input := FamilyAuthenticationInput{}
	switch action.Action {
	case "family-auth-sms":
		if pending.Stage != "repair-consent" {
			return Result{Message: "当前家庭验证不接受重复发送短信"}
		}
		input.Action = "sms"
	case "family-auth-code":
		if pending.Stage != "sms-code" || len(action.Code) != 6 || strings.IndexFunc(action.Code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return Result{Message: "请输入当前家庭验证收到的六位短信验证码"}
		}
		input.Action, input.Code = "code", action.Code
	case "family-auth-cancel":
		if pending.Stage != "repair-consent" && pending.Stage != "sms-code" {
			return Result{Message: "Apple 正在处理本次验证，请等待结果"}
		}
		input.Action = "cancel"
	default:
		return Result{Message: "无效家庭验证操作"}
	}
	select {
	case s.familyInput <- input:
		state := *pending
		state.Stage = "submitting"
		s.familyAuth = &state
		return Result{OK: true, Stage: "family-auth-pending", Message: "验证操作已交接，正在等待 Apple 确认"}
	default:
		return Result{Message: "家庭验证正在处理，请稍候"}
	}
}
