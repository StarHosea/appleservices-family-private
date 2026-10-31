package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerModeRequiresExplicitAuthenticationAndOrigin(t *testing.T) {
	for _, config := range []Config{
		{Listen: "0.0.0.0:0"},
		{Listen: ":0", PublicURL: "http://localhost:18741"},
		{Listen: "0.0.0.0:0", AdminToken: strings.Repeat("s", 40)},
		{Listen: "0.0.0.0:0", PublicURL: "http://localhost/path", AdminToken: strings.Repeat("s", 40)},
	} {
		if server, err := New(config); err == nil {
			server.listener.Close()
			t.Fatal("unsafe server configuration accepted")
		}
	}
}

func TestServerAuthenticationProtectsPageAndAPI(t *testing.T) {
	secret := strings.Repeat("s", 40)
	server, err := New(Config{Dir: t.TempDir(), Listen: "127.0.0.1:0", PublicURL: "https://apple.example", AdminToken: secret})
	if err != nil {
		t.Fatal(err)
	}
	defer server.listener.Close()
	for _, test := range []struct {
		name, path, auth, origin, host string
		want                           int
	}{
		{"page-protected", "/", "", "", "", 401},
		{"api-protected", "/api/view", "", "", "", 401},
		{"bad-token", "/api/view", "Bearer wrong", "", "", 401},
		{"bearer-api", "/api/view", "Bearer " + secret, "", "", 200},
		{"foreign-origin", "/api/view", "Bearer " + secret, "https://attacker.example", "", 403},
		{"foreign-host", "/", "Bearer " + secret, "", "attacker.example", 403},
		{"public-health", "/healthz", "", "", "127.0.0.1:18741", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", server.URL()+test.path, nil)
			r.Header.Set("Authorization", test.auth)
			r.Header.Set("Origin", test.origin)
			if test.host != "" {
				r.Host = test.host
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d, want %d", w.Code, test.want)
			}
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("administrator secret leaked")
			}
		})
	}
	// Browser login still requires the per-process CSRF token for the API.
	for _, csrf := range []string{"", server.token} {
		r := httptest.NewRequest("GET", server.URL()+"/api/view", nil)
		r.SetBasicAuth("admin", secret)
		r.Header.Set("X-App-Token", csrf)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		want := http.StatusForbidden
		if csrf != "" {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Fatal("browser CSRF binding failed")
		}
	}
}

func TestServerAuthenticationUsesConfiguredBasicCredentials(t *testing.T) {
	server, err := New(Config{
		Dir:           t.TempDir(),
		Listen:        "127.0.0.1:0",
		PublicURL:     "https://apple.example",
		AdminToken:    strings.Repeat("t", 40),
		AdminUsername: "mhx",
		AdminPassword: "mhx.PWD.2026",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.listener.Close()

	request := httptest.NewRequest("GET", server.URL()+"/", nil)
	request.SetBasicAuth("mhx", "mhx.PWD.2026")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("configured Basic credentials returned status %d", recorder.Code)
	}

	request = httptest.NewRequest("GET", server.URL()+"/", nil)
	request.SetBasicAuth("admin", "mhx.PWD.2026")
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected legacy username accepted: status %d", recorder.Code)
	}
}
