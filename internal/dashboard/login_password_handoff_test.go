package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	appleservices "github.com/Laky-64/appleservices"
)

func TestSavedLoginPasswordHandoffKeepsResumeModeAndAccountBinding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executables")
	}
	t.Setenv("APPLESERVICES_PASSWORD", "unrelated-parent-password")
	for _, password := range []string{"", "fresh-login-password"} {
		name := "passwordless-startup"
		if password != "" {
			name = "fresh-login-handoff"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			account := &Account{ID: "target", AppleID: "target@example.test", Dir: filepath.Join(root, "account")}
			if err := os.MkdirAll(filepath.Join(account.Dir, "native-adi"), 0700); err != nil {
				t.Fatal(err)
			}
			saved := appleservices.Session{AppleID: account.AppleID, DSID: "test-gsa", Tokens: map[string]string{"com.apple.gs.idms.pet": "test-pet"}}
			accepted := acceptedICloudCheckpoint{Session: appleservices.Session{AppleID: account.AppleID, DSID: "test-icloud", Tokens: map[string]string{"mmeAuthToken": "test-mme"}}, ADSID: saved.DSID, ServiceData: map[string]any{}}
			for name, value := range map[string]any{"session.json": saved, "icloud-accepted.json": accepted} {
				data, _ := json.Marshal(value)
				if err := os.WriteFile(filepath.Join(account.Dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"device.json", "push.json", "native-adi/hw_info.plist"} {
				if err := os.WriteFile(filepath.Join(account.Dir, name), []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Check the child environment without writing any credential to disk.
			worker := `#!/bin/sh
resume=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -dir) shift; dir=$1 ;;
    -resume-session) resume=1 ;;
    *password*) exit 91 ;;
  esac
  shift
done
[ "$resume" = 1 ] || exit 92
[ "$APPLESERVICES_APPLEID" = 'target@example.test' ] || exit 93
[ "${APPLESERVICES_PASSWORD-}" = '` + password + `' ] || exit 94
printf 'verified' > "$dir/child-verified"
`
			workerPath := filepath.Join(root, "worker")
			if err := os.WriteFile(workerPath, []byte(worker), 0700); err != nil {
				t.Fatal(err)
			}
			server := &Server{config: Config{Worker: workerPath}}
			wrongAccount := Account{ID: account.ID, AppleID: "other@example.test", Dir: account.Dir}
			if result := server.startSavedAccountWithPassword(context.Background(), &wrongAccount, password); result.OK {
				t.Fatal("password crossed account checkpoint binding")
			}
			var ok bool
			if password == "" {
				ok = server.startSavedAccount(context.Background(), account).OK
			} else {
				ok = server.startSavedAccountWithPassword(context.Background(), account, password).OK
			}
			if !ok {
				t.Fatal("could not start saved login")
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(account.Dir, "child-verified")); err == nil {
					break
				}
				account.mu.Lock()
				message := account.message
				account.mu.Unlock()
				if message != "" || time.Now().After(deadline) {
					t.Fatal("resumed worker did not receive the bound login environment")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
