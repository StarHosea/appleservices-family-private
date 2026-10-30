// portablecheck verifies the real, account-free APS/ADI runtime in a disposable
// private profile. It never prints a generated code, header or device identity.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	appleservices "github.com/Laky-64/appleservices"
	"github.com/Laky-64/appleservices/aps"
	"github.com/Laky-64/appleservices/nativeadi"
	"github.com/google/uuid"
	"howett.net/plist"
)

func main() {
	parent := flag.String("dir", "/data", "existing parent for a disposable private runtime profile")
	nac := flag.Bool("nac", false, "also perform the account-free NAC exchange")
	flag.Parse()
	if err := check(*parent, *nac); err != nil {
		fmt.Fprintln(os.Stderr, "Portable runtime check failed:", err)
		os.Exit(1)
	}
}

func check(parent string, nac bool) error {
	dir, err := os.MkdirTemp(parent, "portable-check-")
	if err != nil {
		return errors.New("cannot create private check directory")
	}
	defer os.RemoveAll(dir)
	identity := uuid.New()
	device := appleservices.Device{Identifier: identity[:], SerialNumber: "AS" + strings.ToUpper(fmt.Sprintf("%x", identity[:5]))}
	data, _ := json.Marshal(device)
	if err := os.WriteFile(filepath.Join(dir, "device.json"), data, 0600); err != nil {
		return errors.New("cannot save check identity")
	}
	library, _ := filepath.Abs(nativeadi.DefaultLibraryPath())
	if err := nativeadi.ValidateLibrary(library); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	profile := filepath.Join(dir, "native-adi")
	bootstrap := exec.CommandContext(ctx, "python", "tools/mac-push-check.py", "--profile", profile,
		"--device", filepath.Join(dir, "device.json"), "--library", library, "--go-state-out", filepath.Join(dir, "push.json"))
	// The account-free probe never passes inherited account credentials onward.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "APPLESERVICES_") || key == "APPLESERVICES_ADI_LIBRARY" || key == "APPLESERVICES_ADI_FENCE" {
			bootstrap.Env = append(bootstrap.Env, entry)
		}
	}
	var diagnostics bytes.Buffer
	bootstrap.Stdout, bootstrap.Stderr = &diagnostics, &diagnostics
	if bootstrap.Run() != nil {
		return errors.New("account-free APS certificate/token provisioning failed")
	}
	var state aps.State
	data, err = os.ReadFile(filepath.Join(dir, "push.json"))
	if err != nil || json.Unmarshal(data, &state) != nil || len(state.Token) != 32 {
		return errors.New("APS token binding failed")
	}
	var hardware struct {
		Push struct {
			Token []byte `plist:"token"`
		} `plist:"push"`
	}
	data, err = os.ReadFile(filepath.Join(profile, "hw_info.plist"))
	if err != nil {
		return errors.New("APS profile missing")
	}
	if _, err := plist.Unmarshal(data, &hardware); err != nil || !bytes.Equal(hardware.Push.Token, state.Token) {
		return errors.New("APS profile/token mismatch")
	}
	fmt.Println("APS: Apple-issued certificate and 32-byte token verified")
	push, err := aps.NewClient(state)
	if err != nil {
		return errors.New("Go APS certificate/key validation failed")
	}
	pushCtx, pushCancel := context.WithTimeout(ctx, 35*time.Second)
	err = push.Connect(pushCtx)
	pushCancel()
	stableToken := bytes.Equal(push.State().Token, state.Token)
	push.Close()
	if err != nil || !stableToken {
		return errors.New("Go APS courier connection or token persistence failed")
	}
	fmt.Println("APS: native Go courier connected with the same bound token")
	for attempt := 0; attempt < 2; attempt++ {
		client, err := nativeadi.NewClient(nativeadi.Config{LibraryPath: library, ProfileDir: profile, Python: "python",
			Identifier: device.Identifier, Serial: device.SerialNumber, PushToken: state.Token})
		if err != nil {
			return err
		}
		if err := client.Start(ctx); err != nil {
			client.Close()
			return err
		}
		headers, headersErr := client.Headers()
		code, codeErr := client.Code(ctx)
		client.Close()
		if headersErr != nil || codeErr != nil || len(code) != 6 || headers["X-Apple-I-MD"] == "" || headers["X-Apple-I-MD-M"] == "" {
			return errors.New("native ADI headers/code generation failed")
		}
	}
	fmt.Println("ADI: real headers and six-digit generation verified before and after restart")
	if nac {
		runtime := filepath.Join(dir, "nac-runtime")
		if err := os.CopyFS(runtime, os.DirFS(".cache/mac-nac-runtime")); err != nil {
			return errors.New("NAC runtime copy failed")
		}
		transport, _ := filepath.Abs("bin/nacexchange")
		command := exec.CommandContext(ctx, "python", "tools/mac-nac-check.py", "--profile", filepath.Join(profile, "hw_info.plist"),
			"--runtime", runtime, "--encrypt-dll", ".cache/activation-source-review/mac-io-encrypt.so",
			"--validation-out", filepath.Join(dir, "validation.bin"), "--synthetic-missing-board-fields", "--transport", transport)
		command.Env = bootstrap.Env
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		if command.Run() != nil || !strings.Contains(output.String(), `"stage": "NACSign"`) {
			return errors.New("account-free NAC exchange failed")
		}
		fmt.Println("NAC: initialization, Apple session exchange and signing verified")
	}
	fmt.Println("Account login, device trust and Apple acceptance of generated codes require separate account verification")
	return nil
}
