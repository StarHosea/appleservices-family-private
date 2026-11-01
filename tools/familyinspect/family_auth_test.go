package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Laky-64/appleservices/gsa"
)

type repairFixture struct {
	calls []string
	fail  string
	pet   bool
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
		_, err := continueFamilyRepair(ctx, f, "target@example.test", "test-account", familyPending(), reader, out)
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
		{name: "no-pet", input: "{\"action\":\"sms\"}\n{\"action\":\"code\",\"code\":\"123456\"}", wantCalls: "discover,sms,code,result"},
		{name: "rejected-code", input: "{\"action\":\"sms\"}\n{\"action\":\"code\",\"code\":\"123456\"}", fail: "code", pet: true, wantCalls: "discover,sms,code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &repairFixture{pet: tc.pet, fail: tc.fail}
			pending := familyPending()
			if tc.wrongAccount {
				pending.AppleID = "other@example.test"
			}
			var output bytes.Buffer
			if _, err := continueFamilyRepair(context.Background(), f, "target@example.test", "test-account", pending, strings.NewReader(tc.input), &output); err == nil {
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
