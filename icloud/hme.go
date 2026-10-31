package icloud

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Laky-64/appleservices/gsa"
	"github.com/Laky-64/appleservices/internal/srp"
)

const (
	hmeBuildNumber  = "2640Build24"
	hmeOAuthClient  = "d39ba9916b7251055b22c7f910e2ea796ee65e98b2ddecea8f5dde8d9d1a815d"
	hmeWebUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)

var (
	ErrHMEAuthenticationRequired      = errors.New("hide my email web authentication required")
	ErrHMEPasswordRequired            = errors.New("隐藏邮件会话尚未建立或已过期，请验证当前账号密码")
	ErrHMELimitReached                = errors.New("Apple 返回隐藏邮件地址数量已达上限，无法生成新候选")
	ErrHMEWebRepairRequired           = errors.New("Apple 要求先接受当前 iCloud Web 条款")
	ErrHMEWebRepairSessionUnavailable = errors.New("没有可复用的 iCloud Web repair 会话")
)

type HideMyEmailAlias struct {
	Email           string `json:"email"`
	Label           string `json:"label,omitempty"`
	Note            string `json:"note,omitempty"`
	Active          bool   `json:"active"`
	CreateTimestamp int64  `json:"create_timestamp,omitempty"`
	ForwardToEmail  string `json:"forward_to_email,omitempty"`
	Origin          string `json:"origin,omitempty"`
	anonymousID     string
}

type HideMyEmailInfo struct {
	Aliases           []HideMyEmailAlias `json:"aliases"`
	ActiveCount       int                `json:"active_count"`
	InactiveCount     int                `json:"inactive_count"`
	ForwardToEmails   []string           `json:"forward_to_emails,omitempty"`
	SelectedForward   string             `json:"selected_forward_to,omitempty"`
	CheckedAt         time.Time          `json:"checked_at"`
	GenerationLimited bool               `json:"generation_limited,omitempty"`
}

type hmeCookie struct {
	Scope    string    `json:"scope"`
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Path     string    `json:"path,omitempty"`
	Domain   string    `json:"domain,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
}

type hmeSession struct {
	AppleID              string      `json:"apple_id"`
	DSID                 string      `json:"dsid"`
	ClientID             string      `json:"client_id"`
	ServiceURL           string      `json:"service_url"`
	AccountCountry       string      `json:"account_country,omitempty"`
	SetupURL             string      `json:"setup_url,omitempty"`
	WebAuthToken         string      `json:"web_auth_token,omitempty"`
	TrustToken           string      `json:"trust_token,omitempty"`
	AliasCount           int         `json:"alias_count,omitempty"`
	GenerationLimitCount int         `json:"generation_limit_count,omitempty"`
	Cookies              []hmeCookie `json:"cookies"`
	UpdatedAt            time.Time   `json:"updated_at"`
}

type HideMyEmailClient struct {
	appleID     string
	sessionPath string
	authBase    string
	setupBase   string
	http        *http.Client
	jar         http.CookieJar
	session     hmeSession
}

// NewHideMyEmailClient creates an account-bound client. Web cookies are kept
// only in the account's ignored state directory and are never returned to the
// dashboard API.
func NewHideMyEmailClient(accountDir, appleID string) (*HideMyEmailClient, error) {
	appleID = strings.TrimSpace(appleID)
	if accountDir == "" || appleID == "" {
		return nil, errors.New("hide my email account binding is incomplete")
	}
	c := &HideMyEmailClient{
		appleID:     appleID,
		sessionPath: filepath.Join(accountDir, "hme-session.json"),
		authBase:    "https://idmsa.apple.com/appleauth/auth",
		setupBase:   "https://setup.icloud.com/setup/ws/1",
	}
	c.resetHTTP()
	_ = c.loadSession()
	return c, nil
}

func (c *HideMyEmailClient) resetHTTP() {
	jar := newHMECookieJar()
	transport := gsa.AppleRootTransport().(*http.Transport)
	transport.Proxy = nil
	c.jar = jar
	c.http = &http.Client{Jar: jar, Transport: transport, Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func randomHex(bytesCount int) string {
	b := make([]byte, bytesCount)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", bytesCount*2)
	}
	return hex.EncodeToString(b)
}

func (c *HideMyEmailClient) loadSession() error {
	data, err := os.ReadFile(c.sessionPath)
	if err != nil {
		return err
	}
	var saved hmeSession
	if len(data) > 2<<20 || json.Unmarshal(data, &saved) != nil || !strings.EqualFold(saved.AppleID, c.appleID) || saved.ClientID == "" || (saved.WebAuthToken == "" && (saved.DSID == "" || saved.ServiceURL == "")) {
		return errors.New("hide my email session binding is invalid")
	}
	c.session = saved
	if saved.SetupURL == "https://setup.icloud.com/setup/ws/1" || saved.SetupURL == "https://setup.icloud.com.cn/setup/ws/1" {
		c.setupBase = saved.SetupURL
	}
	for _, item := range saved.Cookies {
		u, parseErr := url.Parse(item.Scope)
		if parseErr != nil || u.Scheme != "https" || u.Hostname() == "" || item.Name == "" {
			continue
		}
		c.jar.SetCookies(u, []*http.Cookie{{Name: item.Name, Value: item.Value, Path: item.Path, Domain: item.Domain, Secure: item.Secure, HttpOnly: item.HTTPOnly, Expires: item.Expires}})
	}
	return nil
}

func (c *HideMyEmailClient) saveSession() error {
	c.session.AppleID = c.appleID
	c.session.UpdatedAt = time.Now().UTC()
	c.session.Cookies = nil
	if jar, ok := c.jar.(*hmeCookieJar); ok {
		c.session.Cookies = jar.snapshot()
	}
	scopes := []string{c.authBase, c.setupBase, "https://www.icloud.com", c.session.ServiceURL}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if _, tracked := c.jar.(*hmeCookieJar); tracked {
			break
		}
		u, err := url.Parse(scope)
		if err != nil || u.Hostname() == "" {
			continue
		}
		origin := "https://" + u.Hostname()
		for _, cookie := range c.jar.Cookies(u) {
			key := origin + "\x00" + cookie.Name
			if cookie.Value == "" || seen[key] {
				continue
			}
			seen[key] = true
			c.session.Cookies = append(c.session.Cookies, hmeCookie{Scope: origin, Name: cookie.Name, Value: cookie.Value, Path: cookie.Path, Secure: cookie.Secure, Expires: cookie.Expires})
		}
	}
	data, err := json.Marshal(c.session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.sessionPath), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(c.sessionPath), ".hme-session-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp.Name(), c.sessionPath)
}

type hmeAuthState struct {
	frameID, clientID, srpSession  string
	authAttributes, sessionID      string
	scnt, sessionToken, trustToken string
	grantCode                      string
	accountCountry                 string
}

func (c *HideMyEmailClient) captureAuth(state *hmeAuthState, response *http.Response) {
	for name, target := range map[string]*string{
		"X-Apple-Auth-Attributes":    &state.authAttributes,
		"X-Apple-ID-Session-Id":      &state.sessionID,
		"scnt":                       &state.scnt,
		"X-Apple-Session-Token":      &state.sessionToken,
		"X-Apple-TwoSV-Trust-Token":  &state.trustToken,
		"X-Apple-OAuth-Grant-Code":   &state.grantCode,
		"X-Apple-ID-Account-Country": &state.accountCountry,
	} {
		if value := response.Header.Get(name); value != "" {
			*target = value
		}
	}
}

func hmeFDClientInfo() string {
	return fmt.Sprintf(`{"U":%q,"L":"zh-CN","Z":"GMT+08:00","V":"1.1","F":%q}`, hmeWebUserAgent, "."+randomHex(48))
}

func (c *HideMyEmailClient) authHeaders(state *hmeAuthState) http.Header {
	h := make(http.Header)
	if state.scnt != "" {
		h.Set("scnt", state.scnt)
	}
	if state.sessionID != "" {
		h.Set("X-Apple-ID-Session-Id", state.sessionID)
	}
	if state.authAttributes != "" {
		h.Set("X-Apple-Auth-Attributes", state.authAttributes)
	}
	if state.sessionToken != "" {
		h.Set("X-Apple-Session-Token", state.sessionToken)
	}
	h.Set("X-Apple-OAuth-Client-Id", state.clientID)
	h.Set("X-Apple-OAuth-Client-Type", "firstPartyAuth")
	h.Set("X-Apple-OAuth-Redirect-URI", c.webOrigin())
	h.Set("X-Apple-OAuth-Require-Grant-Code", "true")
	h.Set("X-Apple-OAuth-Response-Mode", "web_message")
	h.Set("X-Apple-OAuth-Response-Type", "code")
	h.Set("X-Apple-OAuth-State", state.frameID)
	h.Set("X-Apple-Widget-Key", state.clientID)
	h.Set("X-Apple-Frame-Id", state.frameID)
	h.Set("X-Apple-Domain-Id", "6")
	h.Set("X-Apple-Locale", "zh_CN")
	h.Set("X-Apple-Offer-Security-Upgrade", "1")
	h.Set("X-Apple-Privacy-Consent", "true")
	h.Set("X-Apple-I-FD-Client-Info", hmeFDClientInfo())
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("Origin", c.authBase)
	h.Set("Referer", c.authBase+"/")
	h.Set("User-Agent", hmeWebUserAgent)
	return h
}

func (c *HideMyEmailClient) authRequest(ctx context.Context, state *hmeAuthState, method, endpoint string, body []byte, headers http.Header) (*http.Response, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, err
	}
	c.captureAuth(state, response)
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	response.Body.Close()
	if readErr != nil || len(data) > 2<<20 {
		return response, nil, errors.New("Apple authentication response was invalid")
	}
	return response, data, nil
}

// Authenticate creates a dedicated iCloud Web session. codeProvider runs only
// after Apple has issued a trusted-device challenge for this same account.
func (c *HideMyEmailClient) Authenticate(ctx context.Context, password string, codeProvider func(context.Context) (string, error)) error {
	return c.authenticate(ctx, password, codeProvider, false)
}

// Refresh re-establishes Web cookies from this account's saved Apple Web token.
// It does not use a password, issue a verification challenge, or accept terms.
func (c *HideMyEmailClient) Refresh(ctx context.Context) error {
	if c.session.WebAuthToken == "" {
		return ErrHMEAuthenticationRequired
	}
	return c.accountLogin(ctx, &hmeAuthState{sessionToken: c.session.WebAuthToken, trustToken: c.session.TrustToken, accountCountry: c.session.AccountCountry}, false)
}

// AuthenticateAndAcceptTerms creates a fresh iCloud Web session and accepts
// the exact iCloud terms version Apple advertises for this account. Callers
// must obtain explicit user authorization immediately before invoking it.
func (c *HideMyEmailClient) AuthenticateAndAcceptTerms(ctx context.Context, password string, codeProvider func(context.Context) (string, error)) error {
	return c.authenticate(ctx, password, codeProvider, true)
}

// AcceptPendingTerms uses a previously saved, account-bound repair session.
// It does not authenticate, create aliases, or alter family membership.
func (c *HideMyEmailClient) AcceptPendingTerms(ctx context.Context) error {
	setupBase := c.setupBase
	locale := "en-us"
	if strings.EqualFold(c.session.AccountCountry, "CHN") || strings.EqualFold(c.session.AccountCountry, "CN") {
		if c.session.SetupURL == "" {
			setupBase = "https://setup.icloud.com.cn/setup/ws/1"
		}
		locale = "zh-cn"
	}
	setupURL, err := url.Parse(setupBase)
	if err != nil {
		return errors.New("iCloud Web repair endpoint is invalid")
	}
	hasRepair := false
	for _, cookie := range c.jar.Cookies(setupURL) {
		if cookie.Name == "X-APPLE-WEBAUTH-REPAIR" && cookie.Value != "" {
			hasRepair = true
			break
		}
	}
	if !hasRepair {
		return ErrHMEWebRepairSessionUnavailable
	}
	if err := c.acceptHMEWebTerms(ctx, setupBase, locale); err != nil {
		return err
	}
	return c.saveSession()
}

func (c *HideMyEmailClient) authenticate(ctx context.Context, password string, codeProvider func(context.Context) (string, error), acceptTerms bool) error {
	if password == "" {
		return ErrHMEPasswordRequired
	}
	return c.authenticateAttempt(ctx, password, codeProvider, acceptTerms)
}

func (c *HideMyEmailClient) webOrigin() string {
	if c.setupBase == "https://setup.icloud.com.cn/setup/ws/1" {
		return "https://www.icloud.com.cn"
	}
	return "https://www.icloud.com"
}

func (c *HideMyEmailClient) authenticateAttempt(ctx context.Context, password string, codeProvider func(context.Context) (string, error), acceptTerms bool) error {
	state := &hmeAuthState{frameID: randomHex(16), clientID: hmeOAuthClient}
	query := url.Values{"frame_id": {"auth-" + state.frameID}, "language": {"en_US"}, "skVersion": {"7"}, "iframeId": {"auth-" + state.frameID}, "client_id": {state.clientID}, "redirect_uri": {c.webOrigin()}, "response_type": {"code"}, "response_mode": {"web_message"}, "state": {"auth-" + state.frameID}, "authVersion": {"latest"}}
	startHeaders := make(http.Header)
	startHeaders.Set("Accept", "*/*")
	startHeaders.Set("User-Agent", hmeWebUserAgent)
	response, _, err := c.authRequest(ctx, state, http.MethodGet, c.authBase+"/authorize/signin?"+query.Encode(), nil, startHeaders)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Apple web authentication could not be initialized")
	}

	federateBody, _ := json.Marshal(map[string]any{"accountName": c.appleID, "rememberMe": true})
	response, _, err = c.authRequest(ctx, state, http.MethodPost, c.authBase+"/federate?isRememberMeEnabled=true", federateBody, c.authHeaders(state))
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Apple web account federation failed")
	}

	params := srp.NewParams()
	srpClient := srp.NewClient(params, c.appleID)
	initBody, _ := json.Marshal(map[string]any{"a": base64.StdEncoding.EncodeToString(srpClient.PublicA().Bytes()), "accountName": c.appleID, "protocols": []string{"s2k", "s2k_fo"}})
	response, initData, err := c.authRequest(ctx, state, http.MethodPost, c.authBase+"/signin/init", initBody, c.authHeaders(state))
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Apple web SRP challenge failed")
	}
	var challenge struct {
		Iteration            int `json:"iteration"`
		Salt, Protocol, B, C string
	}
	if json.Unmarshal(initData, &challenge) != nil || challenge.Iteration <= 0 || challenge.Salt == "" || challenge.B == "" || challenge.C == "" {
		return errors.New("Apple web SRP challenge was incomplete")
	}
	state.srpSession = challenge.C
	salt, saltErr := base64.StdEncoding.DecodeString(challenge.Salt)
	bBytes, bErr := base64.StdEncoding.DecodeString(challenge.B)
	if saltErr != nil || bErr != nil {
		return errors.New("Apple web SRP challenge encoding was invalid")
	}
	passwordHash := sha256.Sum256([]byte(password))
	passwordKey, keyErr := pbkdf2.Key(sha256.New, string(passwordHash[:]), salt, challenge.Iteration, 32)
	if keyErr != nil {
		return keyErr
	}
	x := params.DeriveX(salt, passwordKey)
	m1, _, proofErr := srpClient.ProcessChallenge(x, salt, new(big.Int).SetBytes(bBytes))
	if proofErr != nil {
		return proofErr
	}
	m2, proofErr := srpClient.ExpectedM2()
	if proofErr != nil {
		return proofErr
	}
	trustTokens := []string{}
	if c.session.TrustToken != "" {
		trustTokens = append(trustTokens, c.session.TrustToken)
	}
	completeBody, _ := json.Marshal(map[string]any{"accountName": c.appleID, "rememberMe": true, "trustTokens": trustTokens, "m1": base64.StdEncoding.EncodeToString(m1), "c": state.srpSession, "m2": base64.StdEncoding.EncodeToString(m2)})
	response, _, err = c.authRequest(ctx, state, http.MethodPost, c.authBase+"/signin/complete?isRememberMeEnabled=true", completeBody, c.authHeaders(state))
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusConflict {
		if codeProvider == nil {
			return ErrHMEAuthenticationRequired
		}
		response, _, err = c.authRequest(ctx, state, http.MethodPut, c.authBase+"/verify/trusteddevice/securitycode", nil, c.authHeaders(state))
		if err != nil {
			return errors.New("Apple trusted-device code request failed")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("Apple trusted-device code request failed (HTTP %d)", response.StatusCode)
		}
		codeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		code, codeErr := codeProvider(codeCtx)
		cancel()
		if codeErr != nil || len(strings.TrimSpace(code)) != 6 {
			return errors.New("local trusted-device code generation failed")
		}
		verifyBody, _ := json.Marshal(map[string]any{"securityCode": map[string]string{"code": strings.TrimSpace(code)}})
		response, verifyData, err := c.authRequest(ctx, state, http.MethodPost, c.authBase+"/verify/trusteddevice/securitycode", verifyBody, c.authHeaders(state))
		for index := range verifyBody {
			verifyBody[index] = 0
		}
		code = ""
		if err != nil || !((response.StatusCode >= 200 && response.StatusCode < 300) || (response.StatusCode == http.StatusConflict && state.sessionToken != "" && bytes.Contains(verifyData, []byte(`"valid":true`)))) {
			return errors.New("Apple rejected the trusted-device verification code")
		}
	} else if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Apple web credentials were rejected")
	}
	response, _, err = c.authRequest(ctx, state, http.MethodGet, c.authBase+"/2sv/trust", nil, c.authHeaders(state))
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Apple web session trust failed")
	}
	// Save the verified Web login before setup routing or terms can interrupt
	// activation. A restart can then retry setup without consuming another code.
	c.session.AppleID, c.session.WebAuthToken = c.appleID, state.sessionToken
	c.session.TrustToken, c.session.AccountCountry, c.session.SetupURL = state.trustToken, state.accountCountry, c.setupBase
	if c.session.ClientID == "" {
		c.session.ClientID = randomHex(16)
	}
	if err := c.saveSession(); err != nil {
		return errors.New("无法保存当前账号的 iCloud Web 登录会话")
	}
	return c.accountLogin(ctx, state, acceptTerms)
}

func validateHMEWebCookies(cookies []*http.Cookie) error {
	hasWebSession, requiresRepair := false, false
	for _, cookie := range cookies {
		switch cookie.Name {
		case "X-APPLE-DS-WEB-SESSION-TOKEN", "X-APPLE-WEBAUTH-TOKEN":
			hasWebSession = cookie.Value != ""
		case "X-APPLE-WEBAUTH-REPAIR":
			requiresRepair = cookie.Value != ""
		}
	}
	if hasWebSession {
		return nil
	}
	if requiresRepair {
		return ErrHMEWebRepairRequired
	}
	return errors.New("Apple 未签发 iCloud Web 会话令牌")
}

type hmeAccountLoginResult struct {
	DSInfo struct {
		DSID         string `json:"dsid"`
		LanguageCode string `json:"languageCode"`
	} `json:"dsInfo"`
	Webservices map[string]struct {
		URL string `json:"url"`
	} `json:"webservices"`
	TermsUpdateNeeded bool `json:"termsUpdateNeeded"`
}

func (c *HideMyEmailClient) accountLogin(ctx context.Context, state *hmeAuthState, acceptTerms bool) error {
	setupBase := c.setupBase
	loginBody, _ := json.Marshal(map[string]any{"dsWebAuthToken": state.sessionToken, "accountCountryCode": state.accountCountry, "extended_login": true, "trustToken": state.trustToken})
	data, err := c.hmeSetupRequest(ctx, http.MethodPost, setupBase+"/accountLogin", loginBody)
	var region *hmeRegionRedirectError
	if errors.As(err, &region) && region.setupBase != setupBase {
		// Apple's domainToUse is an application-level routing response. Keep
		// the authenticated IDMS session and change only the setup origin;
		// authenticating twice can consume the same trusted-device code twice.
		c.setupBase, setupBase = region.setupBase, region.setupBase
		data, err = c.hmeSetupRequest(ctx, http.MethodPost, setupBase+"/accountLogin", loginBody)
	}
	if err != nil {
		return fmt.Errorf("Apple iCloud Web session establishment failed: %w", err)
	}
	var result hmeAccountLoginResult
	if json.Unmarshal(data, &result) != nil || result.DSInfo.DSID == "" {
		return errors.New("iCloud+ Hide My Email is unavailable for this account")
	}
	if result.TermsUpdateNeeded {
		c.session.DSID = result.DSInfo.DSID
		c.session.ServiceURL = strings.TrimRight(result.Webservices["premiummailsettings"].URL, "/")
		c.session.SetupURL = setupBase
		if err := c.saveSession(); err != nil {
			return errors.New("无法保存当前账号的 iCloud Web repair 会话")
		}
		if !acceptTerms {
			return ErrHMEWebRepairRequired
		}
		if err := c.acceptHMEWebTerms(ctx, setupBase, result.DSInfo.LanguageCode); err != nil {
			return err
		}
		data, err = c.hmeSetupRequest(ctx, http.MethodPost, setupBase+"/accountLogin", loginBody)
		result = hmeAccountLoginResult{}
		if err != nil || json.Unmarshal(data, &result) != nil || result.TermsUpdateNeeded {
			return errors.New("Apple 未确认 iCloud Web 条款接受状态")
		}
	}
	if result.Webservices["premiummailsettings"].URL == "" {
		return errors.New("iCloud+ Hide My Email is unavailable for this account")
	}
	c.session = hmeSession{AppleID: c.appleID, DSID: result.DSInfo.DSID, ClientID: randomHex(16), ServiceURL: strings.TrimRight(result.Webservices["premiummailsettings"].URL, "/"), AccountCountry: state.accountCountry, SetupURL: setupBase, WebAuthToken: state.sessionToken, TrustToken: state.trustToken, AliasCount: c.session.AliasCount, GenerationLimitCount: c.session.GenerationLimitCount}
	serviceURL, _ := url.Parse(c.session.ServiceURL)
	if err := validateHMEWebCookies(c.jar.Cookies(serviceURL)); err != nil {
		return err
	}
	return c.saveSession()
}

func (c *HideMyEmailClient) hmeSetupRequest(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Origin", c.webOrigin())
	request.Header.Set("Referer", c.webOrigin()+"/")
	request.Header.Set("User-Agent", hmeWebUserAgent)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if readErr != nil || len(data) > 4<<20 {
		return nil, errors.New("Apple iCloud Web setup request failed")
	}
	if response.StatusCode == http.StatusFound {
		return nil, parseHMERegionRedirect(data)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == 421 {
		return nil, ErrHMEAuthenticationRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Apple iCloud Web setup returned HTTP %d", response.StatusCode)
	}
	return data, nil
}

type hmeRegionRedirectError struct{ setupBase string }

func (e *hmeRegionRedirectError) Error() string {
	return "Apple 要求使用账号所属地区的 iCloud Web 登录"
}

func parseHMERegionRedirect(data []byte) error {
	var routing struct {
		Domain string `json:"domainToUse"`
	}
	if json.Unmarshal(data, &routing) == nil {
		switch strings.ToLower(strings.TrimSpace(routing.Domain)) {
		case "icloud.com.cn":
			return &hmeRegionRedirectError{setupBase: "https://setup.icloud.com.cn/setup/ws/1"}
		case "icloud.com":
			return &hmeRegionRedirectError{setupBase: "https://setup.icloud.com/setup/ws/1"}
		}
	}
	return errors.New("Apple 返回了无法识别的 iCloud Web 地区跳转")
}

func (c *HideMyEmailClient) acceptHMEWebTerms(ctx context.Context, setupBase, locale string) error {
	if strings.TrimSpace(locale) == "" {
		locale = "zh-cn"
	}
	body, _ := json.Marshal(map[string]string{"locale": locale})
	data, err := c.hmeSetupRequest(ctx, http.MethodGet, strings.TrimRight(setupBase, "/")+"/getTerms", body)
	if err != nil {
		return errors.New("无法读取 Apple 当前 iCloud Web 条款版本")
	}
	var terms struct {
		ICloudTerms struct {
			Version int `json:"version"`
		} `json:"iCloudTerms"`
	}
	if json.Unmarshal(data, &terms) != nil || terms.ICloudTerms.Version <= 0 {
		return errors.New("Apple 未返回有效的 iCloud Web 条款版本")
	}
	body, _ = json.Marshal(map[string]int{"acceptedICloudTerms": terms.ICloudTerms.Version})
	if _, err = c.hmeSetupRequest(ctx, http.MethodGet, strings.TrimRight(setupBase, "/")+"/repairDone", body); err != nil {
		return errors.New("Apple 未接受 iCloud Web 条款确认")
	}
	return nil
}

type hmeEnvelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		ErrorCode            json.RawMessage `json:"errorCode"`
		ErrorMessage, Reason string
	} `json:"error"`
}

func (c *HideMyEmailClient) request(ctx context.Context, method, path string, payload any, out any) error {
	if c.session.ServiceURL == "" || c.session.DSID == "" {
		return ErrHMEAuthenticationRequired
	}
	endpoint, err := url.Parse(c.session.ServiceURL + path)
	if err != nil {
		return err
	}
	query := endpoint.Query()
	query.Set("clientBuildNumber", hmeBuildNumber)
	query.Set("clientMasteringNumber", hmeBuildNumber)
	query.Set("clientId", c.session.ClientID)
	query.Set("dsid", c.session.DSID)
	endpoint.RawQuery = query.Encode()
	var body io.Reader
	if payload != nil {
		data, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	origin := "https://www.icloud.com"
	if strings.HasSuffix(strings.ToLower(endpoint.Hostname()), ".icloud.com.cn") {
		origin = "https://www.icloud.com.cn"
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Referer", origin+"/")
	request.Header.Set("User-Agent", hmeWebUserAgent)
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "text/plain")
	response, err := c.http.Do(request)
	if err != nil {
		return errors.New("Hide My Email 网络请求失败，请稍后重试")
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	response.Body.Close()
	if readErr != nil || len(data) > 4<<20 {
		return errors.New("Hide My Email response was invalid")
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == 421 {
		return ErrHMEAuthenticationRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Hide My Email returned HTTP %d", response.StatusCode)
	}
	var envelope hmeEnvelope
	if json.Unmarshal(data, &envelope) != nil || !envelope.Success {
		if envelope.Error != nil {
			code := strings.Trim(string(envelope.Error.ErrorCode), `"`)
			if code == "-41012" {
				return ErrHMELimitReached
			}
			if len(code) > 0 && len(code) <= 32 && strings.IndexFunc(code, func(r rune) bool {
				return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_')
			}) < 0 {
				return fmt.Errorf("Hide My Email rejected the request (code %s)", code)
			}
		}
		return errors.New("Hide My Email rejected the request")
	}
	if out != nil && json.Unmarshal(envelope.Result, out) != nil {
		return errors.New("Hide My Email returned an unexpected result")
	}
	_ = c.saveSession()
	return nil
}

func (c *HideMyEmailClient) List(ctx context.Context) (*HideMyEmailInfo, error) {
	var result struct {
		ForwardToEmails []string `json:"forwardToEmails"`
		SelectedForward string   `json:"selectedForwardTo"`
		Emails          []struct {
			HME, Label, Note                    string
			IsActive                            bool
			CreateTimestamp                     int64
			AnonymousID, ForwardToEmail, Origin string
		} `json:"hmeEmails"`
	}
	if err := c.request(ctx, http.MethodGet, "/v2/hme/list", nil, &result); err != nil {
		return nil, err
	}
	info := &HideMyEmailInfo{Aliases: make([]HideMyEmailAlias, 0, len(result.Emails)), ForwardToEmails: append([]string(nil), result.ForwardToEmails...), SelectedForward: result.SelectedForward, CheckedAt: time.Now().UTC()}
	for _, item := range result.Emails {
		alias := HideMyEmailAlias{Email: item.HME, Label: item.Label, Note: item.Note, Active: item.IsActive, CreateTimestamp: item.CreateTimestamp, ForwardToEmail: item.ForwardToEmail, Origin: item.Origin, anonymousID: item.AnonymousID}
		if alias.Active {
			info.ActiveCount++
		} else {
			info.InactiveCount++
		}
		info.Aliases = append(info.Aliases, alias)
	}
	c.session.AliasCount = len(info.Aliases)
	if c.session.GenerationLimitCount > 0 && c.session.AliasCount < c.session.GenerationLimitCount {
		c.session.GenerationLimitCount = 0
	}
	info.GenerationLimited = c.session.GenerationLimitCount > 0
	_ = c.saveSession()
	return info, nil
}

func forwardingTargetAvailable(info *HideMyEmailInfo, target string) bool {
	target = strings.TrimSpace(target)
	if info == nil || target == "" {
		return false
	}
	if strings.EqualFold(target, strings.TrimSpace(info.SelectedForward)) {
		return true
	}
	for _, candidate := range info.ForwardToEmails {
		if strings.EqualFold(target, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

// UpdateForwardTo changes the account-wide Hide My Email forwarding target.
// Apple applies this one target to every alias. The target must be one of the
// account-associated addresses returned by the live list response.
func (c *HideMyEmailClient) UpdateForwardTo(ctx context.Context, target string) error {
	target = strings.TrimSpace(target)
	info, err := c.List(ctx)
	if err != nil {
		return err
	}
	if !forwardingTargetAvailable(info, target) {
		return errors.New("forwarding target is not available for this Apple Account")
	}
	if strings.EqualFold(target, strings.TrimSpace(info.SelectedForward)) {
		return nil
	}
	return c.request(ctx, http.MethodPost, "/v1/hme/updateForwardTo", map[string]string{"forwardToEmail": target}, nil)
}

func (c *HideMyEmailClient) Generate(ctx context.Context, language string) (string, error) {
	if language == "" {
		language = "zh-cn"
	}
	var result struct {
		HME string `json:"hme"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/hme/generate", map[string]string{"langCode": language}, &result); err != nil {
		if errors.Is(err, ErrHMELimitReached) && c.session.AliasCount > 0 {
			c.session.GenerationLimitCount = c.session.AliasCount
			_ = c.saveSession()
		}
		return "", err
	}
	if result.HME == "" {
		return "", errors.New("Hide My Email did not return a candidate")
	}
	c.session.GenerationLimitCount = 0
	_ = c.saveSession()
	return result.HME, nil
}

func (c *HideMyEmailClient) Reserve(ctx context.Context, candidate, label, note string) error {
	if candidate == "" || label == "" {
		return errors.New("candidate and label are required")
	}
	return c.request(ctx, http.MethodPost, "/v1/hme/reserve", map[string]string{"hme": candidate, "label": label, "note": note}, nil)
}

func (c *HideMyEmailClient) findAlias(ctx context.Context, email string) (*HideMyEmailAlias, error) {
	info, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	var match *HideMyEmailAlias
	for index := range info.Aliases {
		if strings.EqualFold(info.Aliases[index].Email, strings.TrimSpace(email)) {
			if match != nil {
				return nil, errors.New("Hide My Email alias binding is ambiguous")
			}
			copy := info.Aliases[index]
			match = &copy
		}
	}
	if match == nil || match.anonymousID == "" {
		return nil, errors.New("Hide My Email alias was not found")
	}
	return match, nil
}

func (c *HideMyEmailClient) Update(ctx context.Context, email, label, note string) error {
	alias, err := c.findAlias(ctx, email)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, "/v1/hme/updateMetaData", map[string]string{"anonymousId": alias.anonymousID, "label": label, "note": note}, nil)
}

func (c *HideMyEmailClient) SetActive(ctx context.Context, email string, active bool) error {
	alias, err := c.findAlias(ctx, email)
	if err != nil {
		return err
	}
	if alias.Active == active {
		return nil
	}
	path := "/v1/hme/deactivate"
	if active {
		path = "/v1/hme/reactivate"
	}
	return c.request(ctx, http.MethodPost, path, map[string]string{"anonymousId": alias.anonymousID}, nil)
}

func (c *HideMyEmailClient) Delete(ctx context.Context, email string) error {
	alias, err := c.findAlias(ctx, email)
	if err != nil {
		return err
	}
	if alias.Active {
		return errors.New("Hide My Email address must be deactivated before deletion")
	}
	return c.request(ctx, http.MethodPost, "/v1/hme/delete", map[string]string{"anonymousId": alias.anonymousID}, nil)
}
