package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Laky-64/appleservices/gsa"
	"github.com/Laky-64/appleservices/icloud"
	"github.com/Laky-64/appleservices/internal/control"
)

// This interface keeps repair on the original organizer-bound GSA client.
// Neither its pending payload nor its PET is written to disk or sent to the UI.
type familyRepairClient interface {
	ResumeRepair(string, *gsa.PendingRepair) (*gsa.LoginResult, error)
	RequestSMSCode(string, string) error
	SubmitSMSCode(string, string, string) error
	VerifiedLoginResult() *gsa.LoginResult
}

func familyAuthProgress(output io.Writer, stage string) error {
	data, _ := json.Marshal(struct {
		Stage string `json:"stage"`
	}{stage})
	_, err := io.WriteString(output, "FAMILY_AUTH_JSON="+string(data)+"\n")
	return err
}

func readFamilyAuthInput(ctx context.Context, decoder *json.Decoder, expected string) (control.FamilyAuthenticationInput, error) {
	type decoded struct {
		input control.FamilyAuthenticationInput
		err   error
	}
	done := make(chan decoded, 1)
	go func() {
		var value control.FamilyAuthenticationInput
		err := decoder.Decode(&value)
		done <- decoded{value, err}
	}()
	select {
	case <-ctx.Done():
		return control.FamilyAuthenticationInput{}, errors.New("家庭验证已过期；请重新核验家庭状态")
	case result := <-done:
		if result.err != nil {
			return control.FamilyAuthenticationInput{}, errors.New("家庭验证交接已关闭")
		}
		if result.input.Action == "cancel" {
			return control.FamilyAuthenticationInput{}, errors.New("家庭验证已取消，未提交加入家庭请求")
		}
		if result.input.Action != expected {
			return control.FamilyAuthenticationInput{}, errors.New("家庭验证步骤不匹配")
		}
		if expected == "code" && (len(result.input.Code) != 6 || strings.IndexFunc(result.input.Code, func(r rune) bool { return r < '0' || r > '9' }) >= 0) {
			return control.FamilyAuthenticationInput{}, errors.New("家庭验证需要六位短信验证码")
		}
		return result.input, nil
	}
}

func continueFamilyRepair(ctx context.Context, client familyRepairClient, appleID, adsid string, pending *gsa.PendingRepair, input io.Reader, output io.Writer) (*gsa.LoginResult, error) {
	if pending == nil || !strings.EqualFold(pending.AppleID, appleID) || pending.Session.DSID != adsid || adsid == "" {
		return nil, errors.New("家庭临时认证的 repair 会话绑定不可用")
	}
	decoder := json.NewDecoder(io.LimitReader(input, 4096))
	decoder.DisallowUnknownFields()
	if err := familyAuthProgress(output, "repair-consent"); err != nil {
		return nil, err
	}
	if _, err := readFamilyAuthInput(ctx, decoder, "sms"); err != nil {
		return nil, err
	}
	// /auth may itself send SMS. Do not discover the repair method before the
	// user explicitly chooses SMS in this exact pending challenge.
	result, err := client.ResumeRepair(appleID, pending)
	if err != nil {
		return nil, familyAuthenticationError("repair-discovery", err)
	}
	if result == nil || !result.NeedsTwoFactor || !result.RequiresSMS || result.SessionPayload["adsid"] != adsid {
		return nil, errors.New("Apple 未返回受支持且绑定当前账号的家庭短信验证")
	}
	identity, _ := result.SessionPayload["GsIdmsToken"].(string)
	if identity == "" {
		return nil, errors.New("家庭短信验证缺少会话绑定")
	}
	// RequestSMSCode reuses an HTTP 201 auto-sent challenge rather than resending.
	if err := client.RequestSMSCode(adsid, identity); err != nil {
		return nil, familyAuthenticationError("repair-sms-request", err)
	}
	if err := familyAuthProgress(output, "sms-code"); err != nil {
		return nil, err
	}
	value, err := readFamilyAuthInput(ctx, decoder, "code")
	if err != nil {
		return nil, err
	}
	err = client.SubmitSMSCode(adsid, identity, value.Code)
	value.Code = ""
	if err != nil {
		return nil, familyAuthenticationError("repair-sms-verify", err)
	}
	result = client.VerifiedLoginResult()
	if result == nil || result.NeedsTwoFactor || result.SessionPayload["adsid"] != adsid {
		return nil, errors.New("Apple 尚未签发绑定当前家庭临时会话的凭据")
	}
	if pet, err := icloud.PETFromSPD(result.SessionPayload); err != nil || pet == "" {
		return nil, errors.New("Apple 尚未签发家庭临时 PET，未提交加入家庭请求")
	}
	return result, nil
}
