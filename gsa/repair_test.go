package gsa

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

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
