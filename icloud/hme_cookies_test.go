package icloud

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestHMECookiesPreserveScopeExpiryAndDeletionAcrossRestart(t *testing.T) {
	jar := newHMECookieJar()
	u, _ := url.Parse("https://setup.icloud.com/setup/ws/1/accountLogin")
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "test-token", Domain: ".icloud.com", Path: "/", Secure: true, HttpOnly: true, Expires: expires},
		{Name: "scoped", Value: "scoped-token", Path: "/setup/", MaxAge: 60},
		{Name: "removed", Value: "old", Path: "/"},
	})
	jar.SetCookies(u, []*http.Cookie{{Name: "removed", Path: "/", MaxAge: -1}})
	saved := jar.snapshot()
	if len(saved) != 2 {
		t.Fatalf("cookie count = %d", len(saved))
	}
	restored := newHMECookieJar()
	for _, item := range saved {
		scope, _ := url.Parse(item.Scope)
		restored.SetCookies(scope, []*http.Cookie{{Name: item.Name, Value: item.Value, Domain: item.Domain, Path: item.Path, Secure: item.Secure, HttpOnly: item.HTTPOnly, Expires: item.Expires}})
		if item.Name == "session" && (item.Domain != ".icloud.com" || !item.Secure || !item.HTTPOnly || !item.Expires.Equal(expires)) {
			t.Fatal("cookie attributes lost")
		}
		if item.Name == "scoped" && (item.Expires.IsZero() || item.Expires.After(time.Now().Add(time.Minute))) {
			t.Fatal("MaxAge was not converted into a persistent expiry")
		}
	}
	service, _ := url.Parse("https://p01-maildomainws.icloud.com/v2/hme/list")
	if cookies := restored.Cookies(service); len(cookies) != 1 || cookies[0].Name != "session" {
		t.Fatal("restored cookie scope changed")
	}
	foreign, _ := url.Parse("https://example.test/")
	if len(restored.Cookies(foreign)) != 0 {
		t.Fatal("Web cookies escaped Apple domain")
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "expired", Value: "old", Expires: time.Now().Add(-time.Hour)}})
	if len(jar.snapshot()) != 2 {
		t.Fatal("expired cookie persisted")
	}
}
