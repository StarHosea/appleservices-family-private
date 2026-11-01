package dashboard

import (
	"context"
	"encoding/json"
	"github.com/Laky-64/appleservices/internal/control"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	appleservices "github.com/Laky-64/appleservices"
)

func TestSetup401ConnectionMakesOneNACDelegateWithoutAcceptingTerms(t *testing.T) {
	t.Run("legacy-python", func(t *testing.T) { testSetup401Connection(t, false, "") })
	t.Run("native-python", func(t *testing.T) { testSetup401Connection(t, true, "") })
	t.Run("late-terms-accepted", func(t *testing.T) { testSetup401Connection(t, true, "true") })
	t.Run("late-terms-refused", func(t *testing.T) { testSetup401Connection(t, true, "false") })
	t.Run("worker-handoff-without-browser", func(t *testing.T) { testSetup401Connection(t, true, "background") })
}

func testSetup401Connection(t *testing.T, nativePython bool, lateTermsReceipt string) {
	t.Helper()
	background := lateTermsReceipt == "background"
	if background {
		lateTermsReceipt = "false"
	}
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executables")
	}
	t.Setenv("APPLESERVICES_NAC_PYTHON", "")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	account := &Account{ID: "test", AppleID: "owner@example.test", Dir: filepath.Join(root, "account")}
	for _, dir := range []string{bin, account.Dir, filepath.Join(root, ".cache", "mac-nac-runtime")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := json.Marshal(appleservices.Session{AppleID: account.AppleID, DSID: "test-account", Tokens: map[string]string{"com.apple.gs.idms.pet": "test-pet"}})
	if err := os.WriteFile(filepath.Join(account.Dir, "session.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	// The setup UI refuses access. No terms are available to approve. The
	// connection may still make one normal v2/NAC exchange, with no consent flag.
	helper := `#!/bin/sh
delegate=0
for arg in "$@"; do
  case "$arg" in
    -delegate-only) delegate=1 ;;
    -terms-accepted|-accept-terms) exit 99 ;;
  esac
done
if [ "$delegate" = 0 ]; then
  printf '{"HTTP":401}\n'
  exit 1
fi
printf 'delegate\n' >> "$0.calls"
printf 'icloud: /setup/signin/v2/login refused (HTTP 401) ErrorID=UNAUTHORIZED\n'
exit 1
`
	if lateTermsReceipt != "" {
		helper = `#!/bin/sh
mode=plan
consent=0
dump=
tuple=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -delegate-only) mode=delegate ;;
    -accept-terms) mode=terms ;;
    -terms-accepted) consent=1 ;;
    -dump) shift; dump=$1 ;;
    -expected-terms) shift; tuple=$1 ;;
  esac
  shift
done
printf '%s\n' "$mode" >> "$0.calls"
if [ "$mode" = plan ]; then
  if [ ! -f "$0.planned" ]; then
    touch "$0.planned"
    printf '{"HTTP":401}\n'
    exit 1
  fi
  printf '{"terms-plan":true,"region":"CN","icloud-version":"123","media-version":"456"}\n'
  exit 0
fi
if [ "$mode" = terms ]; then
  [ "$tuple" = 'CN:123:456' ] || exit 98
  printf '{"success":` + lateTermsReceipt + `}' > "$dump.update.json"
  exit 0
fi
if [ "$consent" = 0 ]; then
  printf 'icloud: refused (HTTP 409) ErrorID=MOBILEME_TERMS_OF_SERVICE_UPDATE\n'
else
  printf 'icloud: refused (HTTP 401) ErrorID=UNAUTHORIZED\n'
fi
exit 1
`
	}
	nac := `#!/bin/sh
printf '%s\n' "$@" > "$0.args"
printf '{"stage": "NACSign"}\n'
`
	nacProgram := filepath.Join(bin, "nacstub")
	if nativePython {
		nacProgram = filepath.Join(bin, "native-nacstub")
		t.Setenv("APPLESERVICES_NAC_PYTHON", nacProgram)
	}
	for path, source := range map[string]string{filepath.Join(bin, "accountsetupcheck"): helper, nacProgram: nac} {
		if err := os.WriteFile(path, []byte(source), 0700); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{config: Config{Worker: filepath.Join(bin, "worker"), Python: filepath.Join(bin, "nacstub"), MacPushSetup: filepath.Join(root, "tools", "mac-push-check.py")}}
	var result control.Result
	if background {
		// A real child process exits at the verified checkpoint. No HTTP
		// request or browser callback is available to initiate activation.
		if err := os.MkdirAll(filepath.Join(account.Dir, "native-adi"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"device.json", "push.json", "native-adi/hw_info.plist"} {
			if err := os.WriteFile(filepath.Join(account.Dir, path), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		worker := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = -dir ]; then shift; dir=$1; fi
  shift
done
printf '{"pid":%s,"running":false,"state":"awaiting_icloud_activation"}' "$$" > "$dir/runtime.json"
`
		if err := os.WriteFile(server.config.Worker, []byte(worker), 0700); err != nil {
			t.Fatal(err)
		}
		started := server.startIn(context.Background(), account.Dir, credentials{AppleID: account.AppleID, Password: "test-password"}, account)
		if !started.OK {
			t.Fatal(started.Message)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			account.mu.Lock()
			message, launching := account.message, account.launching
			account.mu.Unlock()
			if message != "" && !launching {
				result = control.Result{Message: message, Stage: "activation-terms"}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("verified worker exited without completing its server-side handoff")
			}
			time.Sleep(10 * time.Millisecond)
		}
	} else {
		result = server.activationAction(context.Background(), account, true, "")
	}
	wantStage, wantMessage := "activation-delegate", "HTTP 401"
	wantCalls := "delegate\n"
	if lateTermsReceipt != "" {
		wantCalls = "plan\ndelegate\nplan\nterms\n"
		if lateTermsReceipt == "true" {
			wantCalls += "delegate\n"
		} else {
			wantStage, wantMessage = "activation-terms", "Apple 未确认条款接受状态"
		}
	}
	if result.OK || result.Stage != wantStage || !strings.Contains(result.Message, wantMessage) || acceptedCheckpointReady(account.Dir, account.AppleID) {
		t.Fatalf("unaccepted delegate was treated as success: %#v", result)
	}
	calls, err := os.ReadFile(filepath.Join(bin, "accountsetupcheck.calls"))
	if err != nil || string(calls) != wantCalls {
		t.Fatal("connection retried a refused delegate or failed to reach NAC", err)
	}
	args, err := os.ReadFile(nacProgram + ".args")
	budget := "60"
	if runtime.GOOS == "linux" && !nativePython {
		budget = "180"
	}
	if err != nil || !strings.Contains(string(args), "--emulation-timeout-seconds\n"+budget+"\n") {
		t.Fatal("NAC did not receive the platform's bounded emulation budget", err)
	}
	if nativePython != strings.Contains(string(args), "--encrypt-python\n"+filepath.Join(bin, "nacstub")+"\n") {
		t.Fatal("native NAC did not isolate the property routine in the original Python runtime")
	}
}

func TestActivationEnvironmentDoesNotInheritUnverifiedConsentOrNAC(t *testing.T) {
	t.Setenv("APPLESERVICES_TERMS_ACCEPTED", "1")
	t.Setenv("APPLESERVICES_NAC_VALIDATION_FILE", "other-account-validation")
	for _, entry := range filteredActivationEnv("owner@example.test") {
		if strings.HasPrefix(entry, "APPLESERVICES_TERMS_ACCEPTED=") || strings.HasPrefix(entry, "APPLESERVICES_NAC_VALIDATION_FILE=") {
			t.Fatal("activation inherited consent or validation from another operation")
		}
	}
}

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
