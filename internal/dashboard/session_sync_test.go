package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionSyncAPIIsScopedAndIncremental(t *testing.T) {
	key := strings.Repeat("s", 40)
	s, err := New(Config{Dir: t.TempDir(), Port: 0, SyncToken: key})
	if err != nil {
		t.Fatal(err)
	}
	defer s.listener.Close()
	dir := t.TempDir()
	data := []byte(`{"apple_id":"owner@example.test","dsid":"test-dsid","client_id":"test-client","service_url":"https://p42-maildomainws.icloud.com","web_auth_token":"test-auth","trust_token":"test-trust","cookies":[{"scope":"https://setup.icloud.com","domain":".icloud.com","name":"X-APPLE-WEBAUTH-TOKEN","value":"test-cookie"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "hme-session.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s.accounts = []*Account{{ID: "01-owner", AppleID: "owner@example.test", Dir: dir}, {ID: "02-unready", AppleID: "other@example.test", Dir: t.TempDir()}}
	get := func(path, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", s.URL()+path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	const base = "/api/open/v1/sessions"
	for _, token := range []string{"", "wrong"} {
		w := get(base, token, "")
		if w.Code != 401 || strings.Contains(w.Body.String(), "test-cookie") {
			t.Fatal("sync authorization failed")
		}
	}
	if get(base, key, "https://foreign.test").Code != 403 {
		t.Fatal("browser origin accepted")
	}
	w := get(base, key, "")
	var manifest struct {
		Version     int
		Items       []struct{ ID, AppleID, Revision string }
		Unavailable int
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &manifest) != nil || manifest.Version != 1 || len(manifest.Items) != 1 || manifest.Unavailable != 1 {
		t.Fatal("invalid manifest", w.Code)
	}
	for _, secret := range []string{"test-cookie", "test-auth", "test-dsid", "test-trust"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("manifest leaked credentials")
		}
	}
	w = get(base+"/01-owner?revision="+manifest.Items[0].Revision, key, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"authToken":"test-auth"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authorized detail transfer failed")
	}
	if get(base+"/01-owner?revision=old", key, "").Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if get(base+"/missing", key, "").Code != 404 {
		t.Fatal("unknown account accepted")
	}
	if get("/api/view", key, "").Code != 403 {
		t.Fatal("sync key gained dashboard privileges")
	}
	s.config.SyncToken = ""
	if get(base, key, "").Code != 404 {
		t.Fatal("disabled sync API is accessible")
	}
}
