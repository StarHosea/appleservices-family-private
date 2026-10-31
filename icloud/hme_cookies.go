package icloud

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// net/http's jar deliberately omits expiry, domain and path from Cookies.
// Track the original attributes so restarting does not broaden a cookie's
// scope or turn an expired persistent cookie into a new session cookie.
type hmeCookieJar struct {
	http.CookieJar
	mu    sync.Mutex
	saved map[string]hmeCookie
}

func newHMECookieJar() *hmeCookieJar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &hmeCookieJar{CookieJar: jar, saved: make(map[string]hmeCookie)}
}

func (j *hmeCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.CookieJar.SetCookies(u, cookies)
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, c := range cookies {
		if u.Scheme != "https" || c.Name == "" {
			continue
		}
		domain := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		host := strings.ToLower(u.Hostname())
		if domain != "" && domain != host && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		path := c.Path
		if path == "" || path[0] != '/' {
			path = u.Path
			if i := strings.LastIndex(path, "/"); i > 0 {
				path = path[:i]
			} else {
				path = "/"
			}
		}
		keyDomain := domain
		if keyDomain == "" {
			keyDomain = host
		}
		key := keyDomain + "\x00" + path + "\x00" + c.Name
		expires := c.Expires
		if c.MaxAge > 0 {
			expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		}
		if c.MaxAge < 0 || (!expires.IsZero() && !expires.After(now)) {
			delete(j.saved, key)
			continue
		}
		j.saved[key] = hmeCookie{Scope: "https://" + u.Host, Name: c.Name, Value: c.Value, Path: path, Domain: c.Domain, Secure: c.Secure, HTTPOnly: c.HttpOnly, Expires: expires}
	}
}

func (j *hmeCookieJar) snapshot() []hmeCookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	keys := make([]string, 0, len(j.saved))
	for key, c := range j.saved {
		if !c.Expires.IsZero() && !c.Expires.After(time.Now()) {
			delete(j.saved, key)
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cookies := make([]hmeCookie, 0, len(keys))
	for _, key := range keys {
		cookies = append(cookies, j.saved[key])
	}
	return cookies
}
