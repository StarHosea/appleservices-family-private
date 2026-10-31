package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Laky-64/appleservices/icloud"
	"github.com/Laky-64/appleservices/internal/control"
)

func TestReachableVerificationReconcilesAppliedAppleMutation(t *testing.T) {
	info := &icloud.HideMyEmailInfo{ForwardToEmails: []string{"primary@example.test", "new@example.test"}}
	if !reachableVerificationApplied(errors.New("account readback is stale"), info, "NEW@example.test") {
		t.Fatal("Apple-applied reachable email was not reconciled from HME candidates")
	}
	if reachableVerificationApplied(errors.New("verification rejected"), info, "missing@example.test") {
		t.Fatal("missing HME candidate was treated as a successful verification")
	}
}

func TestHMESessionFailureOffersWebAuthentication(t *testing.T) {
	for _, err := range []error{icloud.ErrHMEPasswordRequired, icloud.ErrHMEAuthenticationRequired} {
		result := hmeSessionFailure(err)
		if result.OK || result.Stage != "hme-auth-required" {
			t.Fatalf("missing Web session did not offer authentication: %s", result.Stage)
		}
	}
	if result := hmeSessionFailure(icloud.ErrHMEWebRepairRequired); result.Stage != "hme-terms-required" {
		t.Fatal("terms blocker was confused with missing credentials")
	}
}

func TestRequireHMEFamilyMemberRejectsOrganizerAndStaleCache(t *testing.T) {
	now := time.Now().UTC()
	write := func(dir string, checkedAt time.Time, family control.FamilyInfo) {
		record := hmeFamilyCache{Version: 1, AccountID: filepath.Base(dir), CheckedAt: checkedAt, Family: family}
		data, _ := json.Marshal(record)
		if err := os.WriteFile(filepath.Join(dir, "family-status.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	member := control.FamilyInfo{Member: true, Organizers: 1, Self: 1, StorageKnown: true, StorageParticipating: true, StorageAvailable: true, StorageEnabled: true, StorageBytes: 50 << 30}
	dir := filepath.Join(t.TempDir(), "01-primary")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write(dir, now, member)
	if err := requireHMEFamilyMember(dir, now); err != nil {
		t.Fatal(err)
	}
	member.Organizer = true
	write(dir, now, member)
	if err := requireHMEFamilyMember(dir, now); err == nil {
		t.Fatal("organizer was accepted")
	}
	member.Organizer = false
	write(dir, now.Add(-16*time.Minute), member)
	if err := requireHMEFamilyMember(dir, now); err == nil {
		t.Fatal("stale family cache was accepted")
	}
}

// The container resumes workers without retaining account passwords. Sending a
// reachable-email challenge must offer authentication in that same operation.
type fakeReachableEmailClient struct {
	resumeErr, authErr, sendErr error
	calls                       []string
	password                    string
}

func (c *fakeReachableEmailClient) Resume(context.Context) error {
	c.calls = append(c.calls, "resume")
	return c.resumeErr
}
func (c *fakeReachableEmailClient) Authenticate(_ context.Context, password string, _ func(context.Context) (string, error)) error {
	c.calls = append(c.calls, "authenticate")
	c.password = password
	return c.authErr
}
func (c *fakeReachableEmailClient) BeginReachableEmail(_ context.Context, address, password string) error {
	c.calls = append(c.calls, "send")
	c.password = password
	return c.sendErr
}
func (c *fakeReachableEmailClient) AuthenticationFailure() (string, int) {
	return "test-failure", 403
}

func TestStartReachableEmailAuthentication(t *testing.T) {
	required := icloud.ErrAccountManagementAuthenticationRequired
	rejected := errors.New("Apple rejected request")
	for _, test := range []struct {
		name                        string
		password                    string
		resumeErr, authErr, sendErr error
		wantStage                   string
		wantCalls                   string
		wantOK                      bool
		wantHTTP                    int
	}{
		{name: "resumed worker without management session", resumeErr: required, wantStage: "hme-reachable-auth-required", wantCalls: "resume"},
		{name: "inline password restores management session", password: "test-only-password", resumeErr: required, wantStage: "hme-reachable-code", wantCalls: "resume,authenticate,send", wantOK: true},
		{name: "saved management session needs no password", wantStage: "hme-reachable-code", wantCalls: "resume,send", wantOK: true},
		{name: "saved session requires password for profile edit", sendErr: required, wantStage: "hme-reachable-auth-required", wantCalls: "resume,send"},
		{name: "inline password reaches profile edit", password: "test-only-password", wantStage: "hme-reachable-code", wantCalls: "resume,send", wantOK: true},
		{name: "authentication rejected without sending", password: "test-only-password", resumeErr: required, authErr: rejected, wantStage: "hme-reachable-auth-test-failure", wantCalls: "resume,authenticate", wantHTTP: 403},
		{name: "send rejected is not reported as sent", sendErr: rejected, wantStage: "hme-reachable-test-failure", wantCalls: "resume,send", wantHTTP: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeReachableEmailClient{resumeErr: test.resumeErr, authErr: test.authErr, sendErr: test.sendErr}
			result := startReachableEmail(context.Background(), client, "target@example.test", test.password, nil)
			if result.Stage != test.wantStage || result.OK != test.wantOK || result.HTTPStatus != test.wantHTTP || strings.Join(client.calls, ",") != test.wantCalls {
				t.Fatalf("unexpected challenge result: stage=%s ok=%v status=%d calls=%v", result.Stage, result.OK, result.HTTPStatus, client.calls)
			}
			if test.wantOK && client.password != test.password {
				t.Fatal("inline password did not reach the profile-edit challenge")
			}
		})
	}
}
