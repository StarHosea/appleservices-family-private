package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFamilyAuthenticationInputIsBoundExpiringAndOneShot(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, input, finish := s.BeginFamilyAuthentication("organizer", time.Now().Add(time.Minute))
	defer finish()
	s.SetFamilyAuthenticationStage(id, "repair-consent")
	if s.familyAuthenticationAction(Action{Action: "family-auth-sms", RequestID: "old-challenge"}).OK {
		t.Fatal("accepted stale challenge")
	}
	if len(input) != 0 {
		t.Fatal("sent SMS without explicit request")
	}
	if !s.familyAuthenticationAction(Action{Action: "family-auth-sms", RequestID: id}).OK {
		t.Fatal("SMS choice was not delivered")
	}
	if s.familyAuthenticationAction(Action{Action: "family-auth-sms", RequestID: id}).OK {
		t.Fatal("accepted duplicate SMS request")
	}
	if (<-input).Action != "sms" {
		t.Fatal("wrong action")
	}
	s.SetFamilyAuthenticationStage(id, "sms-code")
	if s.familyAuthenticationAction(Action{Action: "family-auth-code", RequestID: id, Code: "bad"}).OK {
		t.Fatal("accepted invalid code")
	}
	if !s.familyAuthenticationAction(Action{Action: "family-auth-code", RequestID: id, Code: "123456"}).OK {
		t.Fatal("code not delivered")
	}
	value := <-input
	if value.Code != "123456" {
		t.Fatal("code lost")
	}
	value.Code = ""
	if s.familyAuthenticationAction(Action{Action: "family-auth-cancel", RequestID: id}).OK {
		t.Fatal("claimed cancellation after code submission")
	}
	data, _ := json.Marshal(s.familyAuth)
	if string(data) == "" || containsPrivateCode(data) {
		t.Fatal("public state exposed code")
	}
	entries, _ := os.ReadDir(s.dir)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if containsPrivateCode(data) {
			t.Fatal("code persisted")
		}
	}
	finish()
	if s.familyAuthenticationAction(Action{Action: "family-auth-code", RequestID: id, Code: "123456"}).OK {
		t.Fatal("finished challenge accepted input")
	}
	expired, _, clear := s.BeginFamilyAuthentication("organizer", time.Now().Add(-time.Second))
	defer clear()
	s.SetFamilyAuthenticationStage(expired, "repair-consent")
	if s.familyAuthenticationAction(Action{Action: "family-auth-sms", RequestID: expired}).OK {
		t.Fatal("expired challenge accepted")
	}
}
func containsPrivateCode(data []byte) bool {
	for i := 0; i+6 <= len(data); i++ {
		if string(data[i:i+6]) == "123456" {
			return true
		}
	}
	return false
}
