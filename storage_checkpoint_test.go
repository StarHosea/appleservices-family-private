package appleservices

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Laky-64/appleservices/gsa"
	"github.com/Laky-64/appleservices/icloud"
)

type checkpointStore struct {
	memoryStore
	accepted RestoredICloudSession
	owner    string
	writes   int
}

func (s *checkpointStore) SaveAcceptedICloud(owner string, accepted RestoredICloudSession) error {
	s.owner, s.accepted = owner, accepted
	s.writes++
	return nil
}

type checkpointTransport func(*http.Request) (*http.Response, error)

func (f checkpointTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCheckpointICloudRequiresLiveQuotaAndPreservesAccountBinding(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, status := range []int{http.StatusUnauthorized, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			quotaReads := 0
			http.DefaultTransport = checkpointTransport(func(r *http.Request) (*http.Response, error) {
				quotaReads++
				if r.Method != http.MethodGet || r.URL.Host != "quota.icloud.com" || r.Header.Get("X-Apple-ADSID") != "test-adsid" {
					t.Fatal("checkpoint did not use the current account's read-only quota request")
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"storage_data":{"quota_info_in_bytes":{"total_quota":100,"total_used":10,"total_available":90},"storage_info_in_bytes":{}}}`))}, nil
			})
			store := &checkpointStore{}
			backend := &fakeBackend{results: []*gsa.LoginResult{testResult(false)}}
			fetch := func(map[string]string, string, string, string) (icloud.DelegateTokens, error) {
				return icloud.DelegateTokens{DSID: "test-dsid", MMEAuthToken: "private-mme", ServiceData: map[string]any{"storageInfoURL": "https://quota.icloud.com/quota"}}, nil
			}
			login, err := BeginLogin(Credentials{"owner@example.test", "private-password"}, store, testOptions(backend, fetch)...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = login.Client(); err != nil {
				t.Fatal(err)
			}
			info, err := login.CheckpointICloud(context.Background())
			if quotaReads != 1 {
				t.Fatal("live quota was not verified exactly once")
			}
			if status != http.StatusOK {
				if err == nil || store.writes != 0 {
					t.Fatal("failed quota produced an accepted checkpoint")
				}
				return
			}
			if err != nil || info.TotalBytes != 100 || store.writes != 1 || store.owner != "owner@example.test" || store.accepted.ADSID != store.session.DSID || store.accepted.DSID != "test-dsid" || store.accepted.MMEAuthToken != "private-mme" || store.accepted.ServiceData == nil {
				t.Fatal("successful live quota was not saved with its account binding")
			}
		})
	}
}
