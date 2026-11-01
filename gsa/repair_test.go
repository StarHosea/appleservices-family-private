package gsa

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const repairSMSMetadata = `{"authenticationType":"hsa2","hsa2Account":true,"mode":"sms","securityCode":{"length":6},"trustedPhoneNumber":{"id":7},"trustedPhoneNumbers":[{"id":1}]}`

const repairSMSPushMetadata = `{"authenticationType":"hsa2","hsa2Account":true,"securityCode":{"length":6,"securityCodeLocked":false,"tooManyCodesSent":false,"tooManyCodesValidated":false},"trustedPhoneNumber":{"id":7,"pushMode":"sms"},"trustedPhoneNumbers":[{"id":1,"pushMode":"voice"},{"id":7,"pushMode":"sms"}],"hideSendSMSCodeOption":false}`

func TestRepairSMSSelectedPhoneOffer(t *testing.T) {
	if !isRepairSMSChallenge([]byte(repairSMSPushMetadata)) {
		t.Fatal("selected phone SMS offer without top-level mode was rejected")
	}
	for _, body := range []string{
		strings.Replace(repairSMSPushMetadata, `"hideSendSMSCodeOption":false`, `"hideSendSMSCodeOption":true`, 1),
		strings.Replace(repairSMSPushMetadata, `,"hideSendSMSCodeOption":false`, ``, 1),
		strings.Replace(repairSMSPushMetadata, `"pushMode":"sms"`, `"pushMode":"voice"`, 1),
		strings.Replace(repairSMSPushMetadata, `"id":7`, `"id":0`, 1),
		strings.Replace(repairSMSPushMetadata, `"length":6`, `"length":4`, 1),
		strings.Replace(repairSMSPushMetadata, `"tooManyCodesSent":false`, `"tooManyCodesSent":true`, 1),
		strings.Replace(repairSMSPushMetadata, `"securityCodeLocked":false`, `"securityCodeLocked":true`, 1),
		strings.Replace(repairSMSPushMetadata, `"hsa2Account":true`, `"hsa2Account":false`, 1),
		strings.Replace(repairSMSPushMetadata, `"authenticationType":"hsa2"`, `"authenticationType":"hsa2","mode":"voice"`, 1),
		strings.Replace(repairSMSPushMetadata, `"authenticationType":"hsa2"`, `"authenticationType":"hsa2","direct":{"phoneNumberVerification":{"trustedPhoneNumber":{"id":1}}}`, 1),
	} {
		if isRepairSMSChallenge([]byte(body)) {
			t.Fatal("accepted a blocked, unsupported or differently selected phone offer")
		}
	}
}

func TestRepairSelectedPhoneOfferRequestsSMSBeforeVerification(t *testing.T) {
	c := NewClient(headerProvider{})
	requests := 0
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if r.Method != http.MethodGet || r.URL.Path != "/auth" {
				t.Fatal("did not discover the current challenge")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(repairSMSPushMetadata))}, nil
		}
		if requests != 2 || r.Method != http.MethodPut || r.URL.Path != "/auth/verify/phone" {
			t.Fatal("unexpected request instead of sending SMS once")
		}
		var body smsRequestBody
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.PhoneNumber.ID != 7 || body.Mode != "sms" {
			t.Fatal("SMS did not use the selected phone and advertised mode")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	result, err := c.continueRepairSMS(map[string]any{"adsid": "test-account", "GsIdmsToken": "test-identity"})
	if err != nil || result == nil || !result.NeedsTwoFactor || !result.RequiresSMS || requests != 1 {
		t.Fatal("discovery did not preserve the unverified SMS challenge", err)
	}
	if err := c.RequestSMSCode("test-account", "test-identity"); err != nil || requests != 2 || c.VerifiedLoginResult() != nil {
		t.Fatal("SMS request failed or completed authentication without a code and PET", err)
	}
}

func TestRepairSMSRequiresExplicitChallenge(t *testing.T) {
	for _, body := range []string{`{}`, `{"trustedPhoneNumbers":[{"id":7}]}`, `{"termsUpdateNeeded":true}`, strings.Replace(repairSMSMetadata, `"hsa2Account":true`, `"hsa2Account":false`, 1), strings.Replace(repairSMSMetadata, `"mode":"sms"`, `"mode":"voice"`, 1), strings.Replace(repairSMSMetadata, `"length":6`, `"length":6,"securityCodeLocked":true`, 1)} {
		if isRepairSMSChallenge([]byte(body)) {
			t.Fatal("unknown repair or blocked challenge was treated as SMS verification")
		}
	}
	if !isRepairSMSChallenge([]byte(repairSMSMetadata)) {
		t.Fatal("explicit HSA2 SMS challenge was rejected")
	}
}

func TestRepairSMSDiscoveryKeepsAutoSentCodeUntilAppleIssuesPET(t *testing.T) {
	for _, issuePET := range []bool{false, true} {
		c := NewClient(headerProvider{})
		requests := 0
		c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				if r.Method != http.MethodGet || r.URL.Path != "/auth" {
					t.Fatal("repair did not discover Apple's authentication metadata")
				}
				return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(repairSMSMetadata))}, nil
			}
			if r.Method != http.MethodPost || r.URL.Path != "/auth/verify/phone/securitycode" {
				t.Fatal("sent another code instead of using the discovered challenge")
			}
			var body smsSubmitBody
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.PhoneNumber.ID != 7 {
				t.Fatal("code was submitted to an unselected phone")
			}
			headers := http.Header{}
			if issuePET {
				headers.Set("X-Apple-PE-Token", base64.StdEncoding.EncodeToString([]byte("test-account:test-pet")))
			}
			return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})
		payload := map[string]any{"adsid": "test-account", "GsIdmsToken": "test-identity"}
		result, err := c.continueRepairSMS(payload)
		if err != nil || !result.NeedsTwoFactor || !result.RequiresSMS || c.VerifiedLoginResult() != nil {
			t.Fatal("discovered repair challenge was rejected or treated as authenticated", err)
		}
		if err := c.RequestSMSCode("test-account", "test-identity"); err != nil || requests != 1 {
			t.Fatal("automatically sent code was replaced", err)
		}
		if err := c.SubmitSMSCode("test-account", "test-identity", "123456"); err != nil {
			t.Fatal(err)
		}
		if (c.VerifiedLoginResult() != nil) != issuePET {
			t.Fatal("verification completed without Apple's issued PET or discarded a valid PET")
		}
	}
}

func TestResumeRepairKeepsAccountLifetimeAndBinaryPayloadBinding(t *testing.T) {
	c := NewClient(headerProvider{})
	c.anisetteHeaders()
	key := bytes.Repeat([]byte{3}, 32)
	payload := map[string]any{"adsid": "test-account", "GsIdmsToken": "test-identity", "url": "repair", "sk": key, "c": []byte("test-cookie")}
	p := c.accountRepairError("owner@example.test", nil, payload).(*AccountRepairError).Pending
	p.SMS = &RepairSMSChallenge{PhoneID: 7, CodeSent: true}
	encoded, _ := json.Marshal(p)
	var pending PendingRepair
	if json.Unmarshal(encoded, &pending) != nil {
		t.Fatal("checkpoint roundtrip failed")
	}
	resumed := NewClient(headerProvider{})
	resumed.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("resumed an already-sent challenge by sending another request")
		return nil, nil
	})
	result, err := resumed.ResumeRepair("OWNER@example.test", &pending)
	if err != nil || !result.NeedsTwoFactor || !result.RequiresSMS {
		t.Fatal("bound challenge was not resumed", err)
	}
	if restored, ok := result.SessionPayload["sk"].([]byte); !ok || !bytes.Equal(restored, key) {
		t.Fatal("binary session material was corrupted by checkpoint serialization")
	}
	if resumed.RequestSMSCode("test-account", "test-identity") != nil {
		t.Fatal("already-sent code could not be reused")
	}
	if _, err := resumed.ResumeRepair("other@example.test", &pending); err == nil {
		t.Fatal("cross-account challenge was resumed")
	}
	pending.CreatedAt = time.Now().Add(-31 * time.Minute)
	if _, err := resumed.ResumeRepair("owner@example.test", &pending); err == nil {
		t.Fatal("expired challenge was resumed")
	}
}

func TestPendingRepairPreservesChallengeWithoutLoggingCredentials(t *testing.T) {
	c := NewClient(headerProvider{"X-Apple-I-MD": "private-anisette"})
	if _, err := c.anisetteHeaders(); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://appleid.apple.com/auth/repair?token=private-url-token")
	c.cookieJar.SetCookies(u, []*http.Cookie{{Name: "challenge", Value: "private-cookie", Path: "/auth"}})
	payload := map[string]any{"adsid": "private-account-id", "GsIdmsToken": "private-identity", "url": u.String()}
	err := c.accountRepairError("owner@example.test", map[string]any{"au": "repair"}, payload)
	var repair *AccountRepairError
	if !errors.Is(err, ErrAccountRepairRequired) || !errors.As(err, &repair) || repair.Pending == nil {
		t.Fatal("password-verified repair challenge was discarded")
	}
	pending := repair.Pending
	if pending.AppleID != "owner@example.test" || pending.URL != u.String() || pending.IdentityToken != "private-identity" ||
		pending.Session.DSID != "private-account-id" || pending.CreatedAt.IsZero() || len(pending.Session.Tokens) != 0 {
		t.Fatal("repair checkpoint lost its binding or became a successful login session")
	}
	if len(pending.Session.Cookies[u.String()]) != 1 || pending.Session.Cookies[u.String()][0].Value != "private-cookie" {
		t.Fatal("repair-origin cookies were discarded")
	}
	if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "owner@") {
		t.Fatal("repair error exposed credentials")
	}
	delete(payload, "GsIdmsToken")
	if c.accountRepairError("owner@example.test", nil, payload).(*AccountRepairError).Pending != nil {
		t.Fatal("incomplete challenge was checkpointed")
	}
}

func TestRepairURLRejectsUntrustedOrigins(t *testing.T) {
	for _, raw := range []string{"http://appleid.apple.com/auth", "https://appleid.apple.com.evil.test/auth", "https://evil.test/auth", "https://appleid.apple.com@evil.test/", "https://appleid.apple.com:8443/auth", "/auth/repair", "https://setup.icloud.com/repairDone"} {
		if _, ok := RepairURL(raw); ok {
			t.Fatalf("accepted untrusted repair origin: %s", raw)
		}
	}
	for _, raw := range []string{"https://gsa.apple.com/auth", "https://appleid.apple.com/auth/repair?token=test", "https://idmsa.apple.com:443/auth", "https://account.apple.com/"} {
		if _, ok := RepairURL(raw); !ok {
			t.Fatalf("rejected Apple authentication origin: %s", raw)
		}
	}
}

func TestPendingRepairRetainsUnfamiliarURLWithoutFollowingIt(t *testing.T) {
	for _, raw := range []string{"https://setup.icloud.com/setup/update_account_ui", "appleid://repair?token=private-token", "https://unknown.example.test/?token=private-token", "https://invalid.test/%"} {
		c := NewClient(headerProvider{})
		c.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("checkpoint creation fetched an unvalidated URL")
			return nil, nil
		})
		if _, err := c.anisetteHeaders(); err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{"adsid": "private-account", "GsIdmsToken": "private-identity", "url": raw}
		err := c.accountRepairError("owner@example.test", nil, payload).(*AccountRepairError)
		if err.Pending == nil || err.Pending.URL != raw || err.Pending.Payload["GsIdmsToken"] != "private-identity" || strings.Contains(err.Error(), "private-") {
			t.Fatal("unfamiliar continuation was discarded or exposed")
		}
	}
}
