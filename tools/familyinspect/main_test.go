package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Laky-64/appleservices/gsa"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Laky-64/appleservices/internal/control"
)

func TestPrivateMemberAccountsExcludeOtherFamilyDataAndRefuseOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "members.json")
	members := []any{
		map[string]any{"member-apple-id": "organizer@example.test", "member-is-organizer": true, "member-dsid": "private-dsid", "member-phone-numbers": []string{"private-phone"}},
		map[string]any{"appleId": "member@example.test", "is-me": true, "member-date-of-birth": "private-birthday", "member-first-name": "private-name"},
	}
	if err := writeFamilyMemberAccounts(path, members); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-") || strings.Contains(string(data), "dsid") {
		t.Fatal("member output contains fields beyond account and roles")
	}
	var accounts []control.FamilyMember
	if json.Unmarshal(data, &accounts) != nil || len(accounts) != 2 || accounts[0].AppleID != "organizer@example.test" || !accounts[0].Organizer || accounts[1].AppleID != "member@example.test" || !accounts[1].Self {
		t.Fatal("account and role bindings were not preserved")
	}
	if err := writeFamilyMemberAccounts(path, nil); err == nil {
		t.Fatal("existing private member output was overwritten")
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		t.Fatal("member account output must be owner-only")
	}
}

func TestPrivateMemberAccountsKeepMissingOrInvalidAccountsAsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "members.json")
	members := []any{map[string]any{"is-me": true}, map[string]any{"appleId": "invalid\naccount"}}
	if err := writeFamilyMemberAccounts(path, members); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var accounts []control.FamilyMember
	if json.Unmarshal(data, &accounts) != nil || len(accounts) != 2 || accounts[0].AppleID != "" || !accounts[0].Self || accounts[1].AppleID != "" {
		t.Fatal("missing accounts must not omit members or invent identifiers")
	}
}

func TestSafeFamilyUIFormsOmitsValuesAndResolvesAppleAction(t *testing.T) {
	origin, _ := url.Parse("https://setup.icloud.com/setup/mac/family/setupFamilyUI")
	forms := safeFamilyUIForms(origin, []byte(`<html><form method="post" action="/setup/familykit/macos/addFamilyMember"><input type="hidden" name="familyId" value="private"><input name="email" value="private@example.test"><button type="submit" name="continue" value="private">Continue</button></form></html>`))
	if len(forms) != 1 || forms[0].Method != "POST" || forms[0].Host != "setup.icloud.com" || forms[0].Path != "/setup/familykit/macos/addFamilyMember" {
		t.Fatalf("unexpected safe form summary: %#v", forms)
	}
	want := []string{"hidden:familyId", "input:email", "submit:continue"}
	if !reflect.DeepEqual(forms[0].Fields, want) {
		t.Fatalf("values leaked or fields changed: %#v", forms[0].Fields)
	}
}

func TestSafeFamilyUIScriptsAndIdentifiers(t *testing.T) {
	origin, _ := url.Parse("https://setup.icloud.com/setup/mac/family/setupFamilyUI")
	scripts := safeFamilyUIScripts(origin, []byte(`<base href="/resource/current/family/"><script src="javascript-packed.js"></script><script src="https://evil.example/family.js"></script>`))
	if len(scripts) != 1 || scripts[0].Hostname() != "setup.icloud.com" || scripts[0].EscapedPath() != "/resource/current/family/javascript-packed.js" {
		t.Fatalf("unexpected safe scripts: %#v", scripts)
	}
	want := []string{"addFamilyMember", "finishSettingUpFamily"}
	if got := safeFamilyActionIdentifiers([]byte(`function addFamilyMember(secret){return finishSettingUpFamily(secret)}`)); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected safe identifiers: %#v", got)
	}
}

func TestSortedMapKeys(t *testing.T) {
	if got, want := sortedMapKeys(map[string]any{"z": 1, "a": 2}), []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected sorted keys: %#v", got)
	}
}

func TestNumericIDValueAcceptsAppleIntegerShapes(t *testing.T) {
	for _, value := range []any{"123", int(123), int64(123), uint64(123), float64(123)} {
		if got := numericIDValue(value); got != "123" {
			t.Fatalf("unexpected numeric ID %q for %T", got, value)
		}
	}
	for _, value := range []any{"", "12x", 0, -1, 1.5} {
		if got := numericIDValue(value); got != "" {
			t.Fatalf("invalid ID accepted: %q for %T", got, value)
		}
	}
}

func TestFamilyResponseErrorRecognizesHTTP200ErrorPayload(t *testing.T) {
	for _, payload := range []map[string]any{
		{"invalid-invite-code-title": "Invalid"},
		{"status-message": "invalid invitation"},
		{"success": false},
	} {
		if !familyResponseError(payload) {
			t.Fatalf("Apple error payload was accepted: %#v", payload)
		}
	}
	if familyResponseError(map[string]any{"success": true, "status-message": "success"}) {
		t.Fatal("success payload was rejected")
	}
}

func TestFindFamilyBoolMatchesOnlyRedactedSharingFlags(t *testing.T) {
	member := map[string]any{
		"member-apple-id":                "private@example.test",
		"shareMyPurchasesEnabledDefault": false,
		"nested": map[string]any{
			"location-sharing-enabled": true,
		},
	}
	purchase, purchaseKnown := findFamilyBool(member, func(key string) bool {
		return key == "sharemypurchasesenableddefault"
	})
	location, locationKnown := findFamilyBool(member, func(key string) bool {
		return key == "locationsharingenabled"
	})
	if !purchaseKnown || purchase || !locationKnown || !location {
		t.Fatalf("unexpected sharing flags purchase=%t/%t location=%t/%t", purchase, purchaseKnown, location, locationKnown)
	}
}

func TestFindFamilyIntegerAcceptsNestedStorageBytes(t *testing.T) {
	value := map[string]any{"storage": map[string]any{"storageInBytes": float64(53687091200)}}
	if got := findFamilyInteger(value, "storageInBytes"); got != 53687091200 {
		t.Fatalf("unexpected storage bytes %d", got)
	}
}

func TestSummarizeFamilyMembersAndAccountBinding(t *testing.T) {
	members := []any{
		map[string]any{"member-apple-id": "organizer@example.test", "member-is-organizer": true, "is-me": true, "member-dsid": "123"},
		map[string]any{"appleId": "member@example.test", "member-is-child-account": false, "member-dsid": float64(456)},
	}
	organizers, children, me, selfOrganizer, self := summarizeFamilyMembers(members)
	if organizers != 1 || children != 0 || me != 1 || !selfOrganizer || self == nil {
		t.Fatalf("unexpected family summary organizers=%d children=%d me=%d selfOrganizer=%t", organizers, children, me, selfOrganizer)
	}
	if got := familyMemberAppleID(members[1].(map[string]any)); got != "member@example.test" {
		t.Fatalf("unexpected member binding %q", got)
	}
}

func TestUnreadableFamilyNeedsPositiveEligibilityAndWritesEmptyMembers(t *testing.T) {
	for _, input := range []string{`<html>Apple UI error</html>`, `null`, `{}`, `{"eligible":false}`, `{"eligible":true,"success":false}`} {
		if _, err := familyDetailsFromEligibility([]byte(input)); err == nil {
			t.Fatal("unconfirmed eligibility became an empty family")
		}
	}
	details, err := familyDetailsFromEligibility([]byte(`{"eligible":true}`))
	if err != nil || boolValue(details["is-member-of-family"]) {
		t.Fatal("positive eligibility was not normalized")
	}
	path := filepath.Join(t.TempDir(), "members.json")
	members, _ := details["family-members"].([]any)
	if err := writeFamilyMemberAccounts(path, members); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "[]" {
		t.Fatal("empty family must emit a usable private member array")
	}
}

func TestFamilyAuthenticationErrorReportsSafeReasonWithoutPrivateDetails(t *testing.T) {
	private := "private@example.test private-token private-password"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"apple-rejection", fmt.Errorf("private wrapper: %w", &gsa.AuthenticationError{Operation: private, Protocol: private, Code: -20101, ServerMessage: private}), "ec=-20101"},
		{"http", &gsa.AuthenticationError{HTTPStatus: 401, ServerMessage: private}, "HTTP=401"},
		{"repair", fmt.Errorf("%s: %w", private, gsa.ErrAccountRepairRequired), "repair"},
		{"locked", &gsa.SecondFactorError{Operation: private, HTTPStatus: 403, Code: -21669, SecurityCodeLocked: true}, "locked=true"},
		{"transport", &url.Error{Op: "POST", URL: "https://apple.example/" + private, Err: errors.New(private)}, "timeout=false"},
		{"unknown", errors.New(private), "认证协议或设备身份未完成"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := familyAuthenticationError("login", tc.err).Error()
			if !strings.Contains(message, tc.want) || !strings.Contains(message, "未提交加入家庭请求") || strings.Contains(message, "private") {
				t.Fatal("missing safe failure reason or private detail disclosed")
			}
		})
	}
}
