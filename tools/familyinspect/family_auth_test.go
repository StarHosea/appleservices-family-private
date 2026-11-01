package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Laky-64/appleservices/gsa"
)

type repairFixture struct {
	calls       []string
	fail        string
	pet         bool
	loginResult *gsa.LoginResult
	loginErr    error
}

func (f *repairFixture) Login(appleID, password string) (*gsa.LoginResult, error) {
	f.calls = append(f.calls, "login")
	if appleID != "target@example.test" || password != "fixture-password" {
		return nil, errors.New("wrong credential binding")
	}
	return f.loginResult, f.loginErr
}

func (f *repairFixture) ResumeRepair(appleID string, pending *gsa.PendingRepair) (*gsa.LoginResult, error) {
	f.calls = append(f.calls, "discover")
	if f.fail == "discover" {
		return nil, gsa.ErrAccountRepairRequired
	}
	return &gsa.LoginResult{NeedsTwoFactor: true, RequiresSMS: true, SessionPayload: map[string]any{"adsid": pending.Session.DSID, "GsIdmsToken": "private-identity"}}, nil
}
func (f *repairFixture) RequestSMSCode(dsid, identity string) error {
	f.calls = append(f.calls, "sms")
	return nil
}
func (f *repairFixture) SubmitSMSCode(dsid, identity, code string) error {
	f.calls = append(f.calls, "code")
	if dsid != "test-account" || identity != "private-identity" || code != "123456" {
		return errors.New("wrong binding")
	}
	if f.fail == "code" {
		return &gsa.SecondFactorError{Code: -1, HTTPStatus: 400}
	}
	return nil
}
func (f *repairFixture) VerifiedLoginResult() *gsa.LoginResult {
	f.calls = append(f.calls, "result")
	if !f.pet {
		return nil
	}
	return &gsa.LoginResult{SessionPayload: map[string]any{"adsid": "test-account", "t": map[string]any{"com.apple.gs.idms.pet": map[string]any{"token": "private-pet"}}}}
}
func familyPending() *gsa.PendingRepair {
	return &gsa.PendingRepair{AppleID: "target@example.test", Session: gsa.Session{DSID: "test-account"}}
}

func TestFamilyRepairWaitsForConsentAndKeepsOriginalChallengeUntilPET(t *testing.T) {
	f := &repairFixture{pet: true}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	progress := make(chan string, 2)
	out := progressOutput{progress}
	done := make(chan error, 1)
	go func() {
		_, err := continueFamilyRepair(ctx, f, "target@example.test", "test-account", "fixture-password", familyPending(), reader, out)
		done <- err
	}()
	if stage := <-progress; !strings.Contains(stage, "repair-consent") {
		t.Fatal("missing explicit SMS choice")
	}
	// The progress writer synchronizes before input is consumed. No Apple call
	// may run until the caller makes the SMS choice on this same input pipe.
	if len(f.calls) != 0 {
		t.Fatal("discovered or sent SMS before user choice")
	}
	io.WriteString(writer, "{\"action\":\"sms\"}\n")
	if stage := <-progress; !strings.Contains(stage, "sms-code") {
		t.Fatal("missing code prompt")
	}
	if strings.Join(f.calls, ",") != "discover,sms" {
		t.Fatal("did not retain same discovery and SMS challenge")
	}
	io.WriteString(writer, "{\"action\":\"code\",\"code\":\"123456\"}\n")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, ",") != "discover,sms,code,result" {
		t.Fatal("unexpected additional authentication or SMS request")
	}
}

type progressOutput struct{ lines chan string }

func (w progressOutput) Write(p []byte) (int, error) { w.lines <- string(p); return len(p), nil }

func TestFamilyRepairRejectsBindingCancellationUnsupportedRepairAndMissingPET(t *testing.T) {
	for _, tc := range []struct {
		name, input, fail string
		wrongAccount, pet bool
		wantCalls         string
	}{
		{name: "wrong-account", input: `{"action":"sms"}`, wrongAccount: true},
		{name: "cancel", input: `{"action":"cancel"}`},
		{name: "unrecognized-repair", input: `{"action":"sms"}`, fail: "discover", wantCalls: "discover"},
		{name: "no-pet", input: "{\"action\":\"sms\"}\n{\"action\":\"code\",\"code\":\"123456\"}", wantCalls: "discover,sms,code,result,login"},
		{name: "rejected-code", input: "{\"action\":\"sms\"}\n{\"action\":\"code\",\"code\":\"123456\"}", fail: "code", pet: true, wantCalls: "discover,sms,code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &repairFixture{pet: tc.pet, fail: tc.fail}
			pending := familyPending()
			if tc.wrongAccount {
				pending.AppleID = "other@example.test"
			}
			var output bytes.Buffer
			if _, err := continueFamilyRepair(context.Background(), f, "target@example.test", "test-account", "fixture-password", pending, strings.NewReader(tc.input), &output); err == nil {
				t.Fatal("accepted incomplete or unrelated authentication")
			}
			if strings.Join(f.calls, ",") != tc.wantCalls {
				t.Fatal("replayed authentication or crossed account binding")
			}
			if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "123456") {
				t.Fatal("progress exposed authentication material")
			}
		})
	}
}

func TestFamilyRepairFinishesVerifiedSessionWithoutSendingAnotherCode(t *testing.T) {
	verified := &gsa.LoginResult{SessionPayload: map[string]any{"adsid": "test-account", "t": map[string]any{"com.apple.gs.idms.pet": map[string]any{"token": "private-pet"}}}}
	input := "{\"action\":\"sms\"}\n{\"action\":\"code\",\"code\":\"123456\"}"
	f := &repairFixture{loginResult: verified}
	var output bytes.Buffer
	result, err := continueFamilyRepair(context.Background(), f, "target@example.test", "test-account", "fixture-password", familyPending(), strings.NewReader(input), &output)
	if err != nil || result != verified || strings.Join(f.calls, ",") != "discover,sms,code,result,login" {
		t.Fatal("did not complete exactly one same-client handshake after accepting the code", err)
	}
	if strings.Contains(output.String(), "fixture-password") || strings.Contains(output.String(), "123456") {
		t.Fatal("progress exposed credentials")
	}
}

func TestFamilyVerificationFinalHandshakeStopsForIncompleteOrDifferentAccount(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		result   *gsa.LoginResult
		err      error
	}{
		{name: "no-password"},
		{name: "missing-result", password: "fixture-password"},
		{name: "rejected", password: "fixture-password", err: &gsa.AuthenticationError{Code: -1}},
		{name: "new-challenge", password: "fixture-password", result: &gsa.LoginResult{NeedsTwoFactor: true, SessionPayload: map[string]any{"adsid": "test-account"}}},
		{name: "other-account", password: "fixture-password", result: &gsa.LoginResult{SessionPayload: map[string]any{"adsid": "other-account"}}},
		{name: "missing-pet", password: "fixture-password", result: &gsa.LoginResult{SessionPayload: map[string]any{"adsid": "test-account"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &repairFixture{loginResult: tc.result, loginErr: tc.err}
			if _, err := completeFamilyVerification(f, "target@example.test", "test-account", tc.password); err == nil {
				t.Fatal("incomplete final handshake was accepted")
			}
			want := "result,login"
			if tc.password == "" {
				want = "result"
			}
			if strings.Join(f.calls, ",") != want {
				t.Fatal("handshake repeated or requested another code")
			}
		})
	}
}

func TestFamilyAuthenticationKeepsOriginalErrorOnlyInPrivateDiagnostics(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPLESERVICES_2FA_DIAGNOSTIC_DIR", dir)
	original := &gsa.AuthenticationError{HTTPStatus: 401, Code: -123, ServerMessage: "private-account private-original-error"}
	public := familyAuthenticationError("repair-discovery", original).Error()
	if strings.Contains(public, "private-") {
		t.Fatal("original private error entered public output")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("original failure was not retained")
	}
	path := filepath.Join(dir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(original.ServerMessage)) {
		t.Fatal("original Apple error was discarded")
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		t.Fatal("original error must be owner-only")
	}
}

func TestFamilyMutationResponseIsPrivateAndRetainsAppleError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPLESERVICES_2FA_DIAGNOSTIC_DIR", dir)
	body := []byte(`{"error":{"message":"private-original-family-error"}}`)
	traceFamilyAPIResponse("family-member-add", &http.Response{StatusCode: 200, Header: http.Header{"X-Private-Response": {"private-token"}}}, body)
	files, _ := filepath.Glob(filepath.Join(dir, "family-member-add-*.json"))
	if len(files) != 1 {
		t.Fatal("family API response was not retained")
	}
	data, err := os.ReadFile(files[0])
	var result struct {
		Status  int         `json:"status"`
		Body    []byte      `json:"body"`
		Headers http.Header `json:"headers"`
	}
	if err != nil || json.Unmarshal(data, &result) != nil || result.Status != 200 || !bytes.Equal(result.Body, body) || result.Headers.Get("X-Private-Response") != "private-token" {
		t.Fatal("original family API response was changed")
	}
	info, err := os.Stat(files[0])
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("family response must remain owner-only")
	}
}
