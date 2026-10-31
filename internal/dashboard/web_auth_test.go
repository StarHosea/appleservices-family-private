package dashboard

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{Dir: t.TempDir(), Listen: "127.0.0.1:0", PublicURL: "https://apple.example",
		AdminToken: strings.Repeat("t", 40), AdminUsername: "operator", AdminPassword: "test-only-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.listener.Close() })
	return s
}

func authRequest(s *Server, method, path, origin, user, password string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	body := url.Values{"username": {user}, "password": {password}}.Encode()
	r := httptest.NewRequest(method, s.URL()+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	r.Header.Set("X-App-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestWebLoginFormSessionAndLogout(t *testing.T) {
	s := authTestServer(t)
	page := authRequest(s, "GET", "/", "", "", "", nil, "")
	if page.Code != 303 || page.Header().Get("Location") != "/login" || page.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("anonymous browser did not redirect to the form without a Basic challenge")
	}
	page = authRequest(s, "GET", "/login", "", "", "", nil, "")
	if page.Code != 200 || page.Header().Get("Referrer-Policy") != "same-origin" || !strings.Contains(page.Body.String(), `action="/login"`) ||
		!strings.Contains(page.Body.String(), `autocomplete="current-password"`) || strings.Contains(page.Body.String(), s.token) {
		t.Fatal("login form missing or contains dashboard capability")
	}
	style := authRequest(s, "GET", "/login.css", "", "", "", nil, "")
	if style.Code != 200 || !strings.HasPrefix(style.Header().Get("Content-Type"), "text/css") {
		t.Fatal("anonymous login style unavailable")
	}
	for _, origin := range []string{"", "https://attacker.example"} {
		w := authRequest(s, "POST", "/login", origin, "operator", "test-only-password", nil, "")
		if w.Code != 403 || len(w.Result().Cookies()) != 0 {
			t.Fatal("cross-origin login accepted")
		}
	}
	wrong := authRequest(s, "POST", "/login", s.URL(), `<script>alert(1)</script>`, "wrong-password", nil, "")
	if wrong.Code != 401 || !strings.Contains(wrong.Body.String(), "用户名或密码不正确") || strings.Contains(wrong.Body.String(), "<script>") ||
		strings.Contains(wrong.Body.String(), "wrong-password") || wrong.Header().Get("WWW-Authenticate") != "" || len(wrong.Result().Cookies()) != 0 {
		t.Fatal("invalid login was not safely displayed in the form")
	}
	login := authRequest(s, "POST", "/login", s.URL(), "operator", "test-only-password", nil, "")
	cookies := login.Result().Cookies()
	if login.Code != 303 || login.Header().Get("Location") != "/" || len(cookies) != 1 {
		t.Fatal("login did not issue a session")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" ||
		!strings.HasPrefix(cookie.Name, "__Host-") || cookie.MaxAge != 43200 {
		t.Fatal("unsafe session cookie attributes")
	}
	page = authRequest(s, "GET", "/", "", "", "", cookie, "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), s.token) || !strings.Contains(page.Body.String(), `action="/logout"`) {
		t.Fatal("authenticated dashboard unavailable")
	}
	for _, csrf := range []string{"", s.token} {
		w := authRequest(s, "GET", "/api/view", s.URL(), "", "", cookie, csrf)
		want := 403
		if csrf != "" {
			want = 200
		}
		if w.Code != want {
			t.Fatal("session bypassed browser API CSRF requirement")
		}
	}
	if w := authRequest(s, "GET", "/login", "", "", "", cookie, ""); w.Code != 303 {
		t.Fatal("logged-in browser was not redirected")
	}
	if w := authRequest(s, "POST", "/logout", "https://attacker.example", "", "", cookie, ""); w.Code != 403 || !s.webAuthorized(cookieRequest(s, cookie)) {
		t.Fatal("cross-origin logout changed session")
	}
	logout := authRequest(s, "POST", "/logout", s.URL(), "", "", cookie, "")
	if logout.Code != 303 || logout.Result().Cookies()[0].MaxAge != -1 || s.webAuthorized(cookieRequest(s, cookie)) {
		t.Fatal("logout did not revoke the session")
	}
	if w := authRequest(s, "GET", "/api/view", "", "", "", cookie, s.token); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("revoked API session accepted or issued Basic challenge")
	}
}

func cookieRequest(s *Server, cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", s.URL()+"/", nil)
	r.AddCookie(cookie)
	return r
}

func TestWebSessionRotationExpiryAndLoginLimit(t *testing.T) {
	s := authTestServer(t)
	first := authRequest(s, "POST", "/login", s.URL(), "operator", "test-only-password", nil, "").Result().Cookies()[0]
	second := authRequest(s, "POST", "/login", s.URL(), "operator", "test-only-password", first, "").Result().Cookies()[0]
	if first.Value == second.Value || s.webAuthorized(cookieRequest(s, first)) || !s.webAuthorized(cookieRequest(s, second)) {
		t.Fatal("login did not rotate session")
	}
	s.webSessions[sessionHash(second.Value)] = time.Now().Add(-time.Second)
	if s.webAuthorized(cookieRequest(s, second)) {
		t.Fatal("expired session accepted")
	}
	second.Value = strings.Repeat("f", 64)
	if s.webAuthorized(cookieRequest(s, second)) {
		t.Fatal("forged session accepted")
	}
	for i := 0; i < 10; i++ {
		if w := authRequest(s, "POST", "/login", s.URL(), "operator", "incorrect", nil, ""); w.Code != 401 {
			t.Fatal("unexpected invalid-login result")
		}
	}
	w := authRequest(s, "POST", "/login", s.URL(), "operator", "test-only-password", nil, "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("login limit not enforced")
	}
	s.loginWindow = time.Now().Add(-2 * time.Minute)
	if w := authRequest(s, "POST", "/login", s.URL(), "operator", "test-only-password", nil, ""); w.Code != 303 {
		t.Fatal("login limit did not expire")
	}
}
