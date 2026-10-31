package dashboard

import (
	"context"
	"encoding/json"
	"github.com/Laky-64/appleservices/internal/control"
	"os"
	"path/filepath"
	"testing"
	"time"

	appleservices "github.com/Laky-64/appleservices"
)

func TestParseActivationPlanReturnsOnlySafeTermsMetadata(t *testing.T) {
	output := []byte("diagnostic\n" + `{"terms-plan":true,"region":"US","icloud-version":"630101","media-version":"290571"}` + "\n")
	plan, err := parseActivationPlan(output)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Required || activationTuple(plan) != "US:630101:290571" || plan.ICloudTermsURL == "" || plan.MediaTermsURL == "" {
		t.Fatalf("unexpected activation plan: %#v", plan)
	}
}

func TestVerifiedGSACheckpointRequiresAccountBoundPET(t *testing.T) {
	dir := t.TempDir()
	session := appleservices.Session{AppleID: "owner@example.test", DSID: "123", Tokens: map[string]string{"com.apple.gs.idms.pet": "private"}}
	data, _ := json.Marshal(session)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if !verifiedGSACheckpointReady(dir, "OWNER@example.test") {
		t.Fatal("valid account-bound GSA checkpoint was rejected")
	}
	if verifiedGSACheckpointReady(dir, "other@example.test") {
		t.Fatal("GSA checkpoint crossed account binding")
	}
	session.Tokens = nil
	data, _ = json.Marshal(session)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if verifiedGSACheckpointReady(dir, session.AppleID) {
		t.Fatal("checkpoint without PET was accepted")
	}
}

func TestAutomaticActivationReusesVerifiedWorkerWithoutTermsHelper(t *testing.T) {
	dir := t.TempDir()
	executable, _ := os.Executable()
	server, err := New(Config{Dir: dir, Worker: executable, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.listener.Close()
	account := &Account{ID: "test", AppleID: "owner@example.test", Dir: dir}
	saved := appleservices.Session{AppleID: account.AppleID, DSID: "gsa-account", Tokens: map[string]string{"com.apple.gs.idms.pet": "private"}}
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := control.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	data, _ = json.Marshal(Status{PID: os.Getpid(), Running: true, Authenticated: true, State: "online"})
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case command := <-controller.Commands:
			if command.Action.Action != "icloud-checkpoint" {
				command.Reply <- control.Result{}
				return
			}
			checkpoint := acceptedICloudCheckpoint{Session: appleservices.Session{AppleID: saved.AppleID, DSID: "icloud-account", Tokens: map[string]string{"mmeAuthToken": "private-mme"}}, ADSID: saved.DSID, ServiceData: map[string]any{}}
			data, _ := json.Marshal(checkpoint)
			err := os.WriteFile(filepath.Join(dir, "icloud-accepted.json"), data, 0600)
			command.Reply <- control.Result{OK: err == nil, Activation: &control.ActivationInfo{Completed: err == nil}}
		case <-ctx.Done():
		}
	}()
	result := server.activationAction(ctx, account, true, "")
	if !result.OK || result.Activation == nil || !result.Activation.Completed || !acceptedCheckpointReady(dir, account.AppleID) {
		t.Fatal("automatic connection did not reuse the authenticated worker")
	}
}
