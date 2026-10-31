package icloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHMESyncPreservesScopeAndOnlyExportsUsableICloudCookies(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	saved := hmeSession{AppleID: "owner@example.test", DSID: "test-dsid", ClientID: "test-client",
		ServiceURL: "https://p42-maildomainws.icloud.com.cn", AccountCountry: "CHN", WebAuthToken: "test-auth", TrustToken: "test-trust", UpdatedAt: now,
		Cookies: []hmeCookie{
			{Scope: "https://setup.icloud.com.cn", Domain: ".icloud.com.cn", Name: "X-APPLE-WEBAUTH-TOKEN", Value: "test-cookie", Path: "/", Secure: true, HTTPOnly: true, Expires: now.Add(time.Hour)},
			{Scope: "https://setup.icloud.com.cn", Name: "site", Value: "CHN"},
			{Scope: "https://idmsa.apple.com", Name: "myacinfo", Value: "idmsa-only"},
			{Scope: "https://setup.icloud.com.cn", Domain: ".icloud.com.cn", Name: "expired", Value: "old-cookie", Expires: now.Add(-time.Minute)},
		}}
	write := func() {
		t.Helper()
		data, err := json.Marshal(saved)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hme-session.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	out, revision, _, err := ReadHMESyncSession(dir, "OWNER@example.test", now)
	if err != nil || len(out.Cookies) != 2 || len(revision) != 64 || out.AuthToken != "test-auth" || out.TrustToken != "test-trust" {
		t.Fatal("source session or credential conversion failed", err)
	}
	for _, c := range out.Cookies {
		if c.Name == "site" && (c.Domain != "setup.icloud.com.cn" || c.Path != "/") {
			t.Fatal("host-only scope was lost")
		}
		if c.Name == "X-APPLE-WEBAUTH-TOKEN" && (!c.HTTPOnly || !c.Secure || !c.Expires.Equal(now.Add(time.Hour))) {
			t.Fatal("cookie attributes were lost")
		}
	}
	saved.UpdatedAt = now.Add(time.Minute)
	saved.AliasCount = 15
	write()
	_, next, _, err := ReadHMESyncSession(dir, saved.AppleID, now)
	if err != nil || next != revision {
		t.Fatal("read-only metadata caused an unnecessary transfer")
	}
	saved.WebAuthToken = "rotated-auth"
	write()
	_, next, _, err = ReadHMESyncSession(dir, saved.AppleID, now)
	if err != nil || next == revision {
		t.Fatal("credential rotation did not change the revision")
	}
	if _, _, _, err := ReadHMESyncSession(dir, "other@example.test", now); err == nil {
		t.Fatal("cross-account binding accepted")
	}
	if _, _, _, err := ReadHMESyncSession(dir, saved.AppleID, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired authentication cookies accepted")
	}
	saved.ServiceURL = "https://icloud.com.attacker.test"
	write()
	if _, _, _, err := ReadHMESyncSession(dir, saved.AppleID, now); err == nil || strings.Contains(err.Error(), "test-auth") {
		t.Fatal("invalid service host accepted or secret leaked")
	}
}
