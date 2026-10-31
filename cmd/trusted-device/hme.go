package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Laky-64/appleservices/icloud"
	"github.com/Laky-64/appleservices/internal/control"
)

type reachableEmailClient interface {
	Resume(context.Context) error
	Authenticate(context.Context, string, func(context.Context) (string, error)) error
	BeginReachableEmail(context.Context, string, string) error
	AuthenticationFailure() (string, int)
}

func startReachableEmail(ctx context.Context, client reachableEmailClient, address, password string, codeProvider func(context.Context) (string, error)) control.Result {
	authRequired := control.Result{Message: "请验证当前 Apple 账号后发送邮箱验证码；密码仅用于本次验证", Stage: "hme-reachable-auth-required"}
	if err := client.Resume(ctx); err != nil {
		if password == "" {
			return authRequired
		}
		if err := client.Authenticate(ctx, password, codeProvider); err != nil {
			stage, status := client.AuthenticationFailure()
			return control.Result{Message: "Apple 账户管理认证未完成；密码未保存", Stage: "hme-reachable-auth-" + stage, HTTPStatus: status}
		}
	}
	if err := client.BeginReachableEmail(ctx, address, password); err != nil {
		if errors.Is(err, icloud.ErrAccountManagementAuthenticationRequired) {
			return authRequired
		}
		stage, status := client.AuthenticationFailure()
		return control.Result{Message: "Apple 未确认发送目标邮箱验证码；请检查邮箱后再决定是否重试", Stage: "hme-reachable-" + stage, HTTPStatus: status}
	}
	return control.Result{OK: true, Message: "Apple 已向目标邮箱发送验证码；请输入邮件中的验证码完成添加并切换转发", Stage: "hme-reachable-code"}
}

func hmeSessionFailure(err error) control.Result {
	stage := "hme-session"
	if errors.Is(err, icloud.ErrHMEPasswordRequired) || errors.Is(err, icloud.ErrHMEAuthenticationRequired) {
		stage = "hme-auth-required"
	}
	if errors.Is(err, icloud.ErrHMEWebRepairRequired) {
		stage = "hme-terms-required"
	}
	return control.Result{Message: "隐藏邮件地址会话未就绪：" + err.Error(), Stage: stage}
}

type hmeFamilyCache struct {
	Version   int                `json:"version"`
	AccountID string             `json:"account_id"`
	CheckedAt time.Time          `json:"checked_at"`
	Family    control.FamilyInfo `json:"family"`
}

func requireHMEFamilyMember(dir string, now time.Time) error {
	data, err := os.ReadFile(filepath.Join(dir, "family-status.json"))
	if err != nil || len(data) == 0 || len(data) > 64<<10 {
		return errors.New("请先读取当前子号的家庭状态")
	}
	var record hmeFamilyCache
	if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.AccountID != filepath.Base(filepath.Clean(dir)) || record.CheckedAt.IsZero() || record.CheckedAt.After(now.Add(5*time.Minute)) {
		return errors.New("当前子号的家庭状态绑定无效")
	}
	if now.Sub(record.CheckedAt) > 15*time.Minute {
		return errors.New("家庭状态已过期，请先重新核验 Apple")
	}
	family := record.Family
	if !family.Member || family.Organizer || family.Organizers != 1 || family.Self != 1 {
		return errors.New("隐藏邮件地址只允许家庭组普通成员使用")
	}
	if !family.StorageKnown || !family.StorageParticipating || !family.StorageAvailable || !family.StorageEnabled || family.StorageBytes < 50<<30 {
		return errors.New("当前子号尚未启用家庭共享 iCloud+")
	}
	return nil
}
