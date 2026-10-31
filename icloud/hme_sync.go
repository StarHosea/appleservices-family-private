package icloud

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HMESyncSession is the version 1 server-to-server exchange. Never expose it
// through the dashboard view or log it: it contains live authentication data.
type HMESyncSession struct {
	AppleID            string          `json:"appleId"`
	DSID               string          `json:"dsid"`
	ClientID           string          `json:"clientId"`
	HMEURL             string          `json:"hmeUrl"`
	AccountCountryCode string          `json:"accountCountryCode"`
	AuthToken          string          `json:"authToken,omitempty"`
	TrustToken         string          `json:"trustToken,omitempty"`
	Cookies            []HMESyncCookie `json:"cookies"`
}

type HMESyncCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Secure   bool      `json:"secure"`
	HTTPOnly bool      `json:"httpOnly"`
	Expires  time.Time `json:"expires,omitempty"`
}

// ReadHMESyncSession reads an atomic saved Web session, without logging in,
// accepting terms, refreshing tokens, or contacting Apple.
func ReadHMESyncSession(dir, appleID string, now time.Time) (HMESyncSession, string, time.Time, error) {
	var out HMESyncSession
	f, err := os.Open(filepath.Join(dir, "hme-session.json"))
	if err != nil {
		return out, "", time.Time{}, errors.New("HME Web session is unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	var saved hmeSession
	if err != nil || len(data) > 2<<20 || json.Unmarshal(data, &saved) != nil ||
		appleID == "" || !strings.EqualFold(strings.TrimSpace(saved.AppleID), strings.TrimSpace(appleID)) ||
		saved.DSID == "" || saved.ClientID == "" {
		return out, "", time.Time{}, errors.New("HME Web session binding is invalid")
	}
	service, err := url.Parse(saved.ServiceURL)
	if err != nil || service.Scheme != "https" || service.User != nil || !hmeSyncTenant(service.Hostname()) {
		return out, "", time.Time{}, errors.New("HME Web service is unavailable")
	}
	out = HMESyncSession{AppleID: strings.TrimSpace(saved.AppleID), DSID: saved.DSID, ClientID: saved.ClientID,
		HMEURL: saved.ServiceURL, AccountCountryCode: saved.AccountCountry, AuthToken: saved.WebAuthToken, TrustToken: saved.TrustToken,
		Cookies: make([]HMESyncCookie, 0)}
	hasToken := false
	for _, c := range saved.Cookies {
		scope, err := url.Parse(c.Scope)
		if err != nil || scope.Scheme != "https" || !hmeSyncTenant(scope.Hostname()) ||
			c.Name == "" || c.Value == "" || (!c.Expires.IsZero() && !c.Expires.After(now)) {
			continue
		}
		domain := c.Domain
		if domain == "" {
			domain = scope.Hostname()
		}
		host := strings.ToLower(strings.TrimPrefix(domain, "."))
		if !hmeSyncTenant(host) || (host != scope.Hostname() && !strings.HasSuffix(scope.Hostname(), "."+host)) {
			continue
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		out.Cookies = append(out.Cookies, HMESyncCookie{Name: c.Name, Value: c.Value, Domain: domain, Path: path, Secure: c.Secure, HTTPOnly: c.HTTPOnly, Expires: c.Expires})
		if (c.Name == "X-APPLE-WEBAUTH-TOKEN" || c.Name == "X-APPLE-DS-WEB-SESSION-TOKEN") &&
			(host == service.Hostname() || strings.HasSuffix(service.Hostname(), "."+host)) {
			hasToken = true
		}
	}
	if !hasToken {
		return HMESyncSession{}, "", time.Time{}, errors.New("usable HME Web cookies are unavailable")
	}
	sort.Slice(out.Cookies, func(i, j int) bool {
		a, b := out.Cookies[i], out.Cookies[j]
		return a.Domain+"\x00"+a.Path+"\x00"+a.Name < b.Domain+"\x00"+b.Path+"\x00"+b.Name
	})
	// Exclude local UpdatedAt/counts: reads alone must not trigger a transfer.
	canonical, _ := json.Marshal(out)
	sum := sha256.Sum256(canonical)
	return out, hex.EncodeToString(sum[:]), saved.UpdatedAt, nil
}

func hmeSyncTenant(host string) bool {
	host = strings.ToLower(host)
	return host == "icloud.com" || host == "icloud.com.cn" || strings.HasSuffix(host, ".icloud.com") || strings.HasSuffix(host, ".icloud.com.cn")
}
