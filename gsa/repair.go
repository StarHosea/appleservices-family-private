package gsa

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// PendingRepair retains the password-verified continuation independently of a
// successful login session. It contains credentials: persist only in the owning
// account's private directory, never in logs or dashboard responses.
type PendingRepair struct {
	AppleID       string    `json:"apple_id"`
	CreatedAt     time.Time `json:"created_at"`
	URL           string    `json:"url"`
	IdentityToken string    `json:"identity_token"`
	Session       Session   `json:"session"`
}

type AccountRepairError struct {
	Pending    *PendingRepair
	diagnostic string
}

func (e *AccountRepairError) Error() string {
	return fmt.Sprintf("%s (%s)", ErrAccountRepairRequired, e.diagnostic)
}

func (e *AccountRepairError) Unwrap() error { return ErrAccountRepairRequired }

// RepairURL accepts only Apple's authentication origins. Query and fragment
// values may carry credentials and must never be included in diagnostics.
func RepairURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, false
	}
	switch strings.ToLower(u.Hostname()) {
	case "gsa.apple.com", "appleid.apple.com", "idmsa.apple.com", "account.apple.com":
		return u, true
	default:
		return nil, false
	}
}

func (c *Client) accountRepairError(username string, status, payload map[string]any) error {
	e := &AccountRepairError{diagnostic: continuationDiagnostic(status, payload)}
	adsid, _ := payload["adsid"].(string)
	identity, _ := payload["GsIdmsToken"].(string)
	if adsid == "" || identity == "" {
		return e
	}
	var repairURL string
	for _, fields := range []map[string]any{status, payload} {
		for _, key := range []string{"url", "repairUrl", "repairURL"} {
			if raw, _ := fields[key].(string); raw != "" {
				if _, ok := RepairURL(raw); ok {
					repairURL = raw
					break
				}
			}
		}
		if repairURL != "" {
			break
		}
	}
	if repairURL == "" {
		return e
	}
	session, err := c.Snapshot(adsid, nil)
	if err != nil {
		return e
	}
	// Snapshot's fixed endpoints do not include the advertised repair origin.
	u, _ := url.Parse(repairURL)
	for _, cookie := range c.cookieJar.Cookies(u) {
		session.Cookies[repairURL] = append(session.Cookies[repairURL], CookieKV{Name: cookie.Name, Value: cookie.Value})
	}
	e.Pending = &PendingRepair{AppleID: username, CreatedAt: time.Now().UTC(), URL: repairURL, IdentityToken: identity, Session: session}
	return e
}
