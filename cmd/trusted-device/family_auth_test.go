package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Laky-64/appleservices/internal/control"
)

func TestInteractiveFamilyHelperRoutesLiveInputsWhileWorkerIsWaiting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX child fixture")
	}
	dir := t.TempDir()
	panel, err := control.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()
	status, _ := json.Marshal(map[string]any{"pid": os.Getpid()})
	os.WriteFile(filepath.Join(dir, "runtime.json"), status, 0600)
	helper := filepath.Join(dir, "helper")
	script := `#!/bin/sh
[ "$APPLESERVICES_TARGET_PASSWORD" = "fixture-password" ] || exit 90
case "$*" in *-family-auth-stdin*) ;; *) exit 91 ;; esac
printf 'FAMILY_AUTH_JSON={"stage":"repair-consent"}\n'
IFS= read -r input
case "$input" in *'"action":"sms"'*) ;; *) exit 92 ;; esac
printf 'FAMILY_AUTH_JSON={"stage":"sms-code"}\n'
IFS= read -r input
case "$input" in *'"action":"code"'*'"code":"123456"'*) ;; *) exit 93 ;; esac
printf 'FAMILY_STATUS_JSON={"member":true,"members":2,"self":1}\n'
`
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type completion struct {
		output string
		err    error
	}
	done := make(chan completion, 1)
	env := []string{"APPLESERVICES_TARGET_PASSWORD=fixture-password"}
	go func() {
		output, err, finish := runInteractiveFamilyHelper(ctx, panel, "organizer", dir, helper, nil, env)
		finish()
		done <- completion{output, err}
	}()
	getState := func() *control.FamilyAuthentication {
		request, _ := http.NewRequestWithContext(ctx, "GET", panel.Record.URL+"/view", nil)
		request.Header.Set("X-Control-Token", panel.Record.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var view control.View
		if json.NewDecoder(response.Body).Decode(&view) != nil {
			t.Fatal("invalid view")
		}
		return view.FamilyAuthentication
	}
	waitStage := func(stage string) *control.FamilyAuthentication {
		for {
			state := getState()
			if state != nil && state.Stage == stage {
				return state
			}
			select {
			case result := <-done:
				t.Fatalf("helper exited before prompt: %v", result.err)
			case <-ctx.Done():
				t.Fatal("prompt did not arrive")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	send := func(action, code, id string) {
		data, _ := json.Marshal(control.Action{Action: action, Code: code, RequestID: id})
		request, _ := http.NewRequestWithContext(ctx, "POST", panel.Record.URL+"/action", bytes.NewReader(data))
		request.Header.Set("X-Control-Token", panel.Record.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result control.Result
		if json.NewDecoder(response.Body).Decode(&result) != nil || !result.OK {
			t.Fatal("input could not bypass waiting worker loop")
		}
	}
	first := waitStage("repair-consent")
	send("family-auth-sms", "", first.RequestID)
	second := waitStage("sms-code")
	if first.RequestID != second.RequestID || second.Organizer != "organizer" {
		t.Fatal("challenge binding changed")
	}
	send("family-auth-code", "123456", second.RequestID)
	result := <-done
	if result.err != nil {
		t.Fatal("interactive helper did not complete", result.err)
	}
	family, err := parseFamilyStatus(result.output)
	if err != nil || !family.Member || family.Self != 1 {
		t.Fatal("membership output lost")
	}
	if getState() != nil || env[0] != "" {
		t.Fatal("challenge or password environment survived completion")
	}
	if strings.Contains(result.output, "fixture-password") || strings.Contains(result.output, "123456") {
		t.Fatal("helper output disclosed credentials")
	}
	// All fixture runtime files, excluding the test script, remain code-free.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.Name() == "helper" {
			continue
		}
		file, _ := os.Open(filepath.Join(dir, entry.Name()))
		data, _ := io.ReadAll(file)
		file.Close()
		if bytes.Contains(data, []byte("123456")) {
			t.Fatal("code persisted")
		}
	}
}

func TestInteractiveFamilyHelperRetainsTerminalAndCancellationFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX child fixture")
	}
	for _, canceled := range []bool{false, true} {
		name := "exit"
		if canceled {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			panel, err := control.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer panel.Close()
			helper := filepath.Join(dir, "helper")
			script := "#!/bin/sh\nprintf 'fixture-original-terminal-error\\n'\nexit 23\n"
			if canceled {
				script = "#!/bin/sh\nprintf 'fixture-original-terminal-error\\n'\n: > \"$APPLESERVICES_2FA_DIAGNOSTIC_DIR/child-started\"\nexec sleep 20\n"
			}
			if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if canceled {
				go func() {
					// Wait for the child to write output so this exercises a killed
					// process rather than an already-canceled command start.
					for ctx.Err() == nil {
						entries, _ := filepath.Glob(filepath.Join(dir, ".family-auth-diagnostics-*", "child-started"))
						if len(entries) != 0 {
							cancel()
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
				}()
			}
			_, err, finish := runInteractiveFamilyHelper(ctx, panel, "organizer", dir, helper, nil, []string{"APPLESERVICES_TARGET_PASSWORD=fixture-password"})
			finish()
			if err == nil {
				t.Fatal("failed helper was reported as successful")
			}
			files, _ := filepath.Glob(filepath.Join(dir, ".family-auth-diagnostics-*", "helper-result.json"))
			if len(files) != 1 {
				t.Fatal("terminal helper result was discarded")
			}
			data, readErr := os.ReadFile(files[0])
			var result struct {
				Error        string `json:"error"`
				Output       string `json:"output"`
				ContextError string `json:"context_error"`
				ExitCode     int    `json:"exit_code"`
			}
			if readErr != nil || json.Unmarshal(data, &result) != nil || result.Error == "" || !strings.Contains(result.Output, "fixture-original-terminal-error") {
				t.Fatal("original terminal result was lost")
			}
			if canceled && result.ContextError == "" || !canceled && result.ExitCode != 23 {
				t.Fatal("process exit cause was lost")
			}
			info, statErr := os.Stat(files[0])
			if statErr != nil || info.Mode().Perm() != 0600 || bytes.Contains(data, []byte("fixture-password")) {
				t.Fatal("diagnostics exposed permissions or password environment")
			}
		})
	}
}
