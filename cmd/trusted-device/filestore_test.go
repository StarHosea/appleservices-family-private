package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Laky-64/appleservices"
)

func TestLoadAcceptedICloudRequiresSameSavedAccount(t *testing.T) {
	dir := t.TempDir()
	store := fileStore{dir: dir}
	saved := &appleservices.Session{AppleID: "owner@example.test", DSID: "gsa-account", Tokens: map[string]string{"com.apple.gs.idms.pet": "pet"}}
	if err := store.SaveSession(saved); err != nil {
		t.Fatal(err)
	}
	checkpoint := map[string]any{
		"session":      appleservices.Session{AppleID: saved.AppleID, DSID: "icloud-account", Tokens: map[string]string{"mmeAuthToken": "mme", "cloudKitToken": "cloudkit"}},
		"adsid":        saved.DSID,
		"service-data": map[string]any{"quota": true},
	}
	data, _ := json.Marshal(checkpoint)
	if err := os.WriteFile(filepath.Join(dir, "icloud-accepted.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := store.LoadAcceptedICloud()
	if err != nil {
		t.Fatal(err)
	}
	if accepted.DSID != "icloud-account" || accepted.ADSID != saved.DSID || accepted.MMEAuthToken == "" || accepted.ServiceData == nil {
		t.Fatalf("unexpected restored checkpoint: %#v", accepted)
	}
	checkpoint["adsid"] = "other-account"
	data, _ = json.Marshal(checkpoint)
	if err := os.WriteFile(filepath.Join(dir, "icloud-accepted.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAcceptedICloud(); err == nil {
		t.Fatal("cross-account accepted checkpoint was restored")
	}
}

func TestSaveAcceptedICloudRejectsCrossAccountAndRefreshesPrivately(t *testing.T) {
	store := fileStore{dir: t.TempDir()}
	if err := store.SaveSession(&appleservices.Session{AppleID: "owner@example.test", DSID: "gsa-account"}); err != nil {
		t.Fatal(err)
	}
	accepted := appleservices.RestoredICloudSession{DSID: "icloud-account", ADSID: "gsa-account", MMEAuthToken: "current-token", ServiceData: map[string]any{"quota": true}}
	if err := store.SaveAcceptedICloud("OWNER@example.test", accepted); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"other@example.test", "owner@example.test"} {
		wrong := accepted
		if owner == "owner@example.test" {
			wrong.ADSID = "other-account"
		}
		if store.SaveAcceptedICloud(owner, wrong) == nil {
			t.Fatal("cross-account checkpoint accepted")
		}
	}
	accepted.MMEAuthToken = "renewed-token"
	if err := store.SaveAcceptedICloud("owner@example.test", accepted); err != nil {
		t.Fatal(err)
	}
	restored, err := store.LoadAcceptedICloud()
	if err != nil || restored.MMEAuthToken != "renewed-token" {
		t.Fatal("verified session refresh was not installed")
	}
	info, err := os.Stat(store.acceptedPath())
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		t.Fatal("checkpoint must remain owner-only")
	}
}
