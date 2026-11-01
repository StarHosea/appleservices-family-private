package gsa

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"howett.net/plist"
)

// PendingRepair retains the password-verified continuation independently of a
// successful login session. It contains credentials: persist only in the owning
// account's private directory, never in logs or dashboard responses.
type PendingRepair struct {
	AppleID       string              `json:"apple_id"`
	CreatedAt     time.Time           `json:"created_at"`
	URL           string              `json:"url"`
	IdentityToken string              `json:"identity_token"`
	Session       Session             `json:"session"`
	Status        map[string]any      `json:"status,omitempty"`
	Payload       map[string]any      `json:"payload,omitempty"`
	PayloadPlist  []byte              `json:"payload_plist,omitempty"`
	SMS           *RepairSMSChallenge `json:"sms,omitempty"`
}

type RepairSMSChallenge struct {
	PhoneID  int  `json:"phone_id"`
	CodeSent bool `json:"code_sent"`
}

// Only an explicit HSA2 SMS challenge can resolve this repair branch. The
// repair marker, phone presence, or HTTP success alone never establish 2FA.
func isRepairSMSChallenge(body []byte) bool {
	var challenge struct {
		AuthenticationType string `json:"authenticationType"`
		Mode               string `json:"mode"`
		HSA2               bool   `json:"hsa2Account"`
		SecurityCode       struct {
			Length           int  `json:"length"`
			Locked           bool `json:"securityCodeLocked"`
			TooManySent      bool `json:"tooManyCodesSent"`
			TooManyValidated bool `json:"tooManyCodesValidated"`
		} `json:"securityCode"`
	}
	return json.Unmarshal(body, &challenge) == nil && challenge.AuthenticationType == "hsa2" && challenge.HSA2 && challenge.Mode == "sms" &&
		challenge.SecurityCode.Length == 6 && !challenge.SecurityCode.Locked && !challenge.SecurityCode.TooManySent && !challenge.SecurityCode.TooManyValidated
}

func (c *Client) continueRepairSMS(payload map[string]any) (*LoginResult, error) {
	adsid, _ := payload["adsid"].(string)
	identity, _ := payload["GsIdmsToken"].(string)
	if adsid == "" || identity == "" {
		return nil, ErrAccountRepairRequired
	}
	if _, err := c.refreshAnisetteHeaders(); err != nil {
		return nil, err
	}
	if _, err := c.fetchPhoneID(adsid, identity, true); err != nil {
		return nil, err
	}
	c.loginPayload = payload
	return &LoginResult{NeedsTwoFactor: true, TwoFactorMethod: "secondaryAuth", SessionPayload: payload, RequiresSMS: true}, nil
}

func (c *Client) ResumeRepair(appleID string, pending *PendingRepair) (*LoginResult, error) {
	if pending == nil || appleID == "" || !strings.EqualFold(pending.AppleID, appleID) || pending.Session.DSID == "" || pending.IdentityToken == "" ||
		pending.CreatedAt.IsZero() || time.Since(pending.CreatedAt) > 30*time.Minute || time.Until(pending.CreatedAt) > 2*time.Minute {
		return nil, errors.New("gsa: pending repair account binding or lifetime is invalid")
	}
	payload := maps.Clone(pending.Payload)
	if len(pending.PayloadPlist) > 0 {
		if _, err := plist.Unmarshal(pending.PayloadPlist, &payload); err != nil {
			return nil, errors.New("gsa: pending repair payload is invalid")
		}
	} else {
		// The initial checkpoint version used JSON only. Restore its two known
		// binary SPD fields without interpreting other account fields as data.
		for _, key := range []string{"sk", "c"} {
			if encoded, ok := payload[key].(string); ok {
				if data, err := base64.StdEncoding.DecodeString(encoded); err == nil {
					payload[key] = data
				}
			}
		}
	}
	if payload["adsid"] != pending.Session.DSID || payload["GsIdmsToken"] != pending.IdentityToken {
		return nil, errors.New("gsa: pending repair payload belongs to a different challenge")
	}
	for raw, cookies := range pending.Session.Cookies {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() != "gsa.apple.com" {
			continue
		}
		for _, cookie := range cookies {
			c.cookieJar.SetCookies(u, []*http.Cookie{{Name: cookie.Name, Value: cookie.Value}})
		}
	}
	c.smsPhoneID, c.smsCodeAlreadySent = 0, false
	c.verifiedTokens = nil
	if pending.SMS != nil && pending.SMS.PhoneID > 0 {
		c.smsPhoneID, c.smsCodeAlreadySent = pending.SMS.PhoneID, pending.SMS.CodeSent
		c.loginPayload = payload
		return &LoginResult{NeedsTwoFactor: true, TwoFactorMethod: "secondaryAuth", SessionPayload: payload, RequiresSMS: true}, nil
	}
	return c.continueRepairSMS(payload)
}

type AccountRepairError struct {
	Pending    *PendingRepair
	diagnostic string
}

func (e *AccountRepairError) Error() string {
	return fmt.Sprintf("%s (%s)", ErrAccountRepairRequired, e.diagnostic)
}

func (e *AccountRepairError) Unwrap() error { return ErrAccountRepairRequired }

// RepairURL validates an origin before using a continuation for an HTTP request.
// Saving a private checkpoint does not fetch or open the URL and must also
// retain unfamiliar origins and native schemes for offline protocol diagnosis.
// Query and fragment values must never be included in diagnostics.
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
				repairURL = raw
				break
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
	if u, err := url.Parse(repairURL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		for _, cookie := range c.cookieJar.Cookies(u) {
			session.Cookies[repairURL] = append(session.Cookies[repairURL], CookieKV{Name: cookie.Name, Value: cookie.Value})
		}
	}
	e.Pending = &PendingRepair{AppleID: username, CreatedAt: time.Now().UTC(), URL: repairURL, IdentityToken: identity, Session: session, Status: status, Payload: payload}
	e.Pending.PayloadPlist, _ = plist.Marshal(payload, plist.BinaryFormat)
	return e
}
