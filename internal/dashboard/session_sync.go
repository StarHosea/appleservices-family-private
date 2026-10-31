package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Laky-64/appleservices/icloud"
)

func (s *Server) serveSessionSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.config.SyncToken == "" {
		http.Error(w, `{"message":"session synchronization is disabled"}`, http.StatusNotFound)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !equalSecret(token, s.config.SyncToken) {
		http.Error(w, `{"message":"invalid session sync token"}`, http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(w, `{"message":"server-to-server requests only"}`, http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, `{"message":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.registryNote != "" {
		http.Error(w, `{"message":"account registry is unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	const base = "/api/open/v1/sessions"
	if r.URL.Path == base {
		type entry struct {
			ID        string    `json:"id"`
			AppleID   string    `json:"appleId"`
			Revision  string    `json:"revision"`
			UpdatedAt time.Time `json:"updatedAt"`
		}
		s.accountsMu.Lock()
		accounts := append([]*Account(nil), s.accounts...)
		s.accountsMu.Unlock()
		items := make([]entry, 0, len(accounts))
		unavailable := 0
		for _, account := range accounts {
			// Account identity can be updated by a reconnect; copy it under its lock.
			account.mu.Lock()
			dir, appleID, id := account.Dir, account.AppleID, account.ID
			account.mu.Unlock()
			session, revision, updatedAt, err := icloud.ReadHMESyncSession(dir, appleID, time.Now())
			if err != nil {
				unavailable++
				continue
			}
			items = append(items, entry{ID: id, AppleID: session.AppleID, Revision: revision, UpdatedAt: updatedAt})
		}
		json.NewEncoder(w).Encode(struct {
			Version     int     `json:"version"`
			Items       []entry `json:"items"`
			Unavailable int     `json:"unavailable"`
		}{1, items, unavailable})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, base+"/")
	if !idPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	account := s.lookup(id)
	if account == nil {
		http.NotFound(w, r)
		return
	}
	account.mu.Lock()
	dir, appleID := account.Dir, account.AppleID
	account.mu.Unlock()
	session, revision, _, err := icloud.ReadHMESyncSession(dir, appleID, time.Now())
	if err != nil {
		http.Error(w, `{"message":"HME Web session is unavailable"}`, http.StatusConflict)
		return
	}
	if expected := r.URL.Query().Get("revision"); expected != "" && expected != revision {
		http.Error(w, `{"message":"session changed; refresh the manifest"}`, http.StatusConflict)
		return
	}
	json.NewEncoder(w).Encode(struct {
		Version  int                   `json:"version"`
		Revision string                `json:"revision"`
		Session  icloud.HMESyncSession `json:"session"`
	}{1, revision, session})
}
