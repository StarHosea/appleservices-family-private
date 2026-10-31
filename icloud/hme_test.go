package icloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type hmeTestTransport func(*http.Request) (*http.Response, error)

func (f hmeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHMERestoresPartialLoginAndRefreshesCookiesWithoutPassword(t *testing.T) {
	dir := t.TempDir()
	client, _ := NewHideMyEmailClient(dir, "member@example.test")
	client.session = hmeSession{AppleID: client.appleID, ClientID: "test-client", WebAuthToken: "test-web-auth", TrustToken: "test-trust", AccountCountry: "CHN"}
	if err := client.saveSession(); err != nil {
		t.Fatal(err)
	}
	client, _ = NewHideMyEmailClient(dir, "member@example.test")
	setupCalls := 0
	transport := hmeTestTransport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{"success":true,"result":{"hmeEmails":[]}}`
		headers := make(http.Header)
		if r.URL.Path == "/setup/ws/1/accountLogin" {
			setupCalls++
			var login map[string]any
			if json.NewDecoder(r.Body).Decode(&login) != nil || login["dsWebAuthToken"] != "test-web-auth" || login["trustToken"] != "test-trust" {
				t.Fatal("saved Web login was not reused")
			}
			if r.URL.Host == "setup.icloud.com" {
				status = http.StatusFound
				body = `{"domainToUse":"iCloud.com.cn"}`
			} else {
				if r.URL.Host != "setup.icloud.com.cn" || r.Header.Get("Origin") != "https://www.icloud.com.cn" {
					t.Fatal("regional setup origin was not switched")
				}
				body = `{"dsInfo":{"dsid":"test-dsid"},"webservices":{"premiummailsettings":{"url":"https://p01-maildomainws.icloud.com.cn"}}}`
				headers.Add("Set-Cookie", "X-APPLE-WEBAUTH-TOKEN=test-cookie; Domain=.icloud.com.cn; Path=/; Secure; HttpOnly")
			}
		} else if r.URL.Path == "/v2/hme/list" {
			if cookie, err := r.Cookie("X-APPLE-WEBAUTH-TOKEN"); err != nil || cookie.Value != "test-cookie" {
				t.Fatal("refreshed cookie missing")
			}
		} else {
			t.Fatal("Web refresh attempted a new authentication challenge")
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	client.http.Transport = transport
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if setupCalls != 2 {
		t.Fatalf("setup calls = %d", setupCalls)
	}
	// A second process needs only the refreshed private session file.
	restored, _ := NewHideMyEmailClient(dir, "member@example.test")
	restored.http.Transport = transport
	if _, err := restored.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restored.setupBase != "https://setup.icloud.com.cn/setup/ws/1" || restored.session.WebAuthToken == "" {
		t.Fatal("routing or renewal token did not survive restart")
	}
}

func TestHMERejectsUnrecognizedRegionRedirect(t *testing.T) {
	for _, body := range []string{`{"domainToUse":"attacker.example"}`, `{"domainToUse":"icloud.com.cn.attacker.example"}`, `{"domainToUse":"https://icloud.com.cn"}`} {
		var redirect *hmeRegionRedirectError
		if errors.As(parseHMERegionRedirect([]byte(body)), &redirect) {
			t.Fatal("unrecognized redirect accepted")
		}
	}
}

func TestHMEGenerationExplainsAppleStringAndNumericQuotaErrors(t *testing.T) {
	for _, code := range []string{`"-41012"`, `-41012`} {
		client, _ := NewHideMyEmailClient(t.TempDir(), "member@example.test")
		client.session = hmeSession{DSID: "test-dsid", ClientID: "test-client", ServiceURL: "https://p01-maildomainws.icloud.com"}
		client.http.Transport = hmeTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"success":false,"error":{"errorCode":` + code + `,"errorMessage":"Maximum aliases"}}`)), Request: r}, nil
		})
		if _, err := client.Generate(context.Background(), "zh-cn"); !errors.Is(err, ErrHMELimitReached) {
			t.Fatalf("quota error was lost: %v", err)
		}
	}
}

func TestHMEGenerationLimitSurvivesRestartAndClearsAfterSmallerList(t *testing.T) {
	dir := t.TempDir()
	count := 750
	transport := hmeTestTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"success":false,"error":{"errorCode":"-41012"}}`
		if r.URL.Path == "/v2/hme/list" {
			aliases := make([]map[string]any, count)
			for i := range aliases {
				aliases[i] = map[string]any{"hme": "alias@example.test", "isActive": true}
			}
			data, _ := json.Marshal(map[string]any{"success": true, "result": map[string]any{"hmeEmails": aliases}})
			body = string(data)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	client, _ := NewHideMyEmailClient(dir, "member@example.test")
	client.session = hmeSession{AppleID: client.appleID, DSID: "test-dsid", ClientID: "test-client", ServiceURL: "https://p01-maildomainws.icloud.com"}
	client.http.Transport = transport
	if _, err := client.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), "zh-cn"); !errors.Is(err, ErrHMELimitReached) {
		t.Fatal("expected quota rejection")
	}
	restored, _ := NewHideMyEmailClient(dir, "member@example.test")
	restored.http.Transport = transport
	info, err := restored.List(context.Background())
	if err != nil || !info.GenerationLimited {
		t.Fatal("verified quota state was lost on restart")
	}
	count = 749
	info, err = restored.List(context.Background())
	if err != nil || info.GenerationLimited {
		t.Fatal("smaller authoritative list did not allow retrying generation")
	}
}

func TestHMEPasswordlessRestartReusesAccountWebSession(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("X-APPLE-WEBAUTH-TOKEN")
		if err != nil || cookie.Value != "test-web-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/hme/list" {
			ioResult := map[string]any{"success": true, "result": map[string]any{"hmeEmails": []any{}}}
			_ = json.NewEncoder(w).Encode(ioResult)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"hme": "candidate@example.test"}})
	}))
	defer server.Close()
	dir := t.TempDir()
	client, err := NewHideMyEmailClient(dir, "member@example.test")
	if err != nil {
		t.Fatal(err)
	}
	client.session = hmeSession{AppleID: client.appleID, DSID: "test-dsid", ClientID: "test-client", ServiceURL: server.URL}
	u, _ := url.Parse(server.URL)
	client.jar.SetCookies(u, []*http.Cookie{{Name: "X-APPLE-WEBAUTH-TOKEN", Value: "test-web-token", Secure: true}})
	if err := client.saveSession(); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(filepath.Join(dir, "hme-session.json"))
	if stat.Mode().Perm() != 0600 {
		t.Fatal("Web session is not private")
	}
	restored, _ := NewHideMyEmailClient(dir, "MEMBER@example.test")
	restored.http.Transport = server.Client().Transport
	info, err := restored.List(context.Background())
	if err != nil || info == nil || len(info.Aliases) != 0 {
		t.Fatalf("passwordless empty list failed: %v", err)
	}
	if _, err := restored.Generate(context.Background(), "zh-cn"); err != nil {
		t.Fatalf("passwordless candidate generation failed: %v", err)
	}
	other, _ := NewHideMyEmailClient(dir, "other@example.test")
	if _, err := other.List(context.Background()); !errors.Is(err, ErrHMEAuthenticationRequired) {
		t.Fatal("another account reused the saved Web session")
	}
}

func TestHMEAuthenticationWithoutPasswordRequestsCredentials(t *testing.T) {
	client, _ := NewHideMyEmailClient(t.TempDir(), "member@example.test")
	if err := client.Authenticate(context.Background(), "", nil); !errors.Is(err, ErrHMEPasswordRequired) {
		t.Fatalf("unexpected missing-password result: %v", err)
	}
}

func TestHideMyEmailLifecycleBindsCurrentAlias(t *testing.T) {
	var mu sync.Mutex
	active := true
	selectedForward := "primary@example.test"
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dsid") != "123" || r.URL.Query().Get("clientBuildNumber") != hmeBuildNumber {
			http.Error(w, `{"success":false}`, http.StatusBadRequest)
			return
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/hme/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"forwardToEmails": []string{"primary@example.test", "backup@example.test"}, "selectedForwardTo": selectedForward, "hmeEmails": []map[string]any{{"hme": "alias@icloud.com", "label": "shop", "note": "test", "isActive": active, "anonymousId": "private-id", "forwardToEmail": selectedForward}}}})
		case "/v1/hme/generate":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"hme": "candidate@icloud.com"}})
		case "/v1/hme/reserve", "/v1/hme/updateMetaData", "/v1/hme/reactivate", "/v1/hme/delete":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		case "/v1/hme/updateForwardTo":
			var request map[string]string
			if json.NewDecoder(r.Body).Decode(&request) != nil || request["forwardToEmail"] != "backup@example.test" {
				t.Fatal("forward target request was not bound to the selected address")
			}
			selectedForward = request["forwardToEmail"]
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		case "/v1/hme/deactivate":
			active = false
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &HideMyEmailClient{
		appleID:     "member@example.test",
		sessionPath: filepath.Join(t.TempDir(), "hme-session.json"),
		http:        server.Client(),
		jar:         jar,
		session:     hmeSession{AppleID: "member@example.test", DSID: "123", ClientID: "client", ServiceURL: server.URL},
	}
	client.http.Jar = jar
	ctx := context.Background()
	info, err := client.List(ctx)
	if err != nil || info.ActiveCount != 1 || len(info.Aliases) != 1 || info.Aliases[0].anonymousID != "private-id" || info.SelectedForward != "primary@example.test" || len(info.ForwardToEmails) != 2 {
		t.Fatalf("unexpected list info=%#v err=%v", info, err)
	}
	candidate, err := client.Generate(ctx, "zh-cn")
	if err != nil || candidate != "candidate@icloud.com" {
		t.Fatalf("unexpected candidate=%q err=%v", candidate, err)
	}
	if err := client.Reserve(ctx, candidate, "label", "note"); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateForwardTo(ctx, "unverified@example.test"); err == nil {
		t.Fatal("unverified forwarding target was accepted")
	}
	if err := client.UpdateForwardTo(ctx, "backup@example.test"); err != nil {
		t.Fatal(err)
	}
	forwarded, err := client.List(ctx)
	if err != nil || forwarded.SelectedForward != "backup@example.test" || forwarded.Aliases[0].ForwardToEmail != "backup@example.test" {
		t.Fatalf("forwarding change was not verified: %#v err=%v", forwarded, err)
	}
	if err := client.Update(ctx, "alias@icloud.com", "updated", "updated note"); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "alias@icloud.com"); err == nil {
		t.Fatal("active alias was deleted")
	}
	if err := client.SetActive(ctx, "alias@icloud.com", false); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "alias@icloud.com"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) < 8 || paths[len(paths)-1] != "/v1/hme/delete" {
		t.Fatalf("unexpected request paths %v", paths)
	}
}

func TestHideMyEmailSafeAliasOmitsPrivateIdentifier(t *testing.T) {
	data, err := json.Marshal(HideMyEmailAlias{Email: "alias@icloud.com", anonymousID: "private-id"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"email":"alias@icloud.com","active":false}` {
		t.Fatalf("private identifier leaked in %s", data)
	}
}

func TestValidateHMEWebCookiesExplainsRepairState(t *testing.T) {
	if err := validateHMEWebCookies([]*http.Cookie{{Name: "X-APPLE-WEBAUTH-TOKEN", Value: "token"}}); err != nil {
		t.Fatal(err)
	}
	err := validateHMEWebCookies([]*http.Cookie{{Name: "X-APPLE-WEBAUTH-REPAIR", Value: "repair"}})
	if !errors.Is(err, ErrHMEWebRepairRequired) {
		t.Fatalf("repair state was not explained: %v", err)
	}
}

func TestAcceptHMEWebTermsUsesAdvertisedVersion(t *testing.T) {
	var accepted int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getTerms":
			if r.Method != http.MethodGet {
				t.Fatalf("getTerms method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"iCloudTerms": map[string]any{"version": 634301}})
		case "/repairDone":
			if r.Method != http.MethodGet {
				t.Fatalf("repairDone method = %s", r.Method)
			}
			var body struct {
				Accepted int `json:"acceptedICloudTerms"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("repairDone body was not valid JSON")
			}
			accepted = body.Accepted
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &HideMyEmailClient{http: server.Client(), jar: jar}
	if err := client.acceptHMEWebTerms(context.Background(), server.URL, "zh-cn"); err != nil {
		t.Fatal(err)
	}
	if accepted != 634301 {
		t.Fatalf("accepted terms version = %d", accepted)
	}
}

func TestAcceptPendingTermsRequiresRepairCookie(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	client := &HideMyEmailClient{setupBase: "https://setup.icloud.com/setup/ws/1", http: http.DefaultClient, jar: jar}
	if err := client.AcceptPendingTerms(context.Background()); !errors.Is(err, ErrHMEWebRepairSessionUnavailable) {
		t.Fatalf("missing repair session was not rejected: %v", err)
	}
}
