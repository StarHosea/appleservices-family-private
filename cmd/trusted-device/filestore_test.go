package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Laky-64/appleservices"
	"github.com/Laky-64/appleservices/gsa"
)

func TestPendingRepairIsPrivateAndSeparateFromSuccessfulSession(t *testing.T) {
	store := fileStore{dir: t.TempDir()}
	saved := &appleservices.Session{AppleID: "owner@example.test", DSID: "old-account", Tokens: map[string]string{"com.apple.gs.idms.pet": "existing-pet"}}
	if err := store.SaveSession(saved); err != nil {
		t.Fatal(err)
	}
	pending := &gsa.PendingRepair{AppleID: saved.AppleID, CreatedAt: time.Now(), URL: "https://appleid.apple.com/auth/repair?token=private", IdentityToken: "private-token", Session: gsa.Session{DSID: "pending-account"}}
	if store.SavePendingRepair("different@example.test", pending) == nil {
		t.Fatal("cross-account repair checkpoint accepted")
	}
	if err := store.SavePendingRepair("OWNER@example.test", pending); err != nil {
		t.Fatal(err)
	}
	pending.URL = "appleid://repair?token=private-native-continuation"
	if err := store.SavePendingRepair("owner@example.test", pending); err != nil {
		t.Fatal("private checkpoint rejected an unfamiliar native continuation", err)
	}
	data, err := os.ReadFile(filepath.Join(store.dir, "gsa-repair.json"))
	var retained gsa.PendingRepair
	if err != nil || json.Unmarshal(data, &retained) != nil || retained.URL != pending.URL {
		t.Fatal("native continuation was not retained privately")
	}
	info, err := os.Stat(filepath.Join(store.dir, "gsa-repair.json"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		t.Fatal("repair checkpoint must remain owner-only")
	}
	restored, err := store.LoadSession()
	if err != nil || restored.DSID != saved.DSID || restored.Tokens["com.apple.gs.idms.pet"] != "existing-pet" {
		t.Fatal("pending repair overwrote the successful login session")
	}
}

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
