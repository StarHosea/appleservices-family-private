package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Laky-64/appleservices/internal/control"
)

func TestDeviceCacheRejectsOtherWorkspaceAndLogin(t *testing.T) {
	account := &Account{ID: "01-account", AppleID: "owner@example.test", Dir: t.TempDir()}
	checkedAt := time.Now().UTC()
	if err := writeDeviceCache(account, []control.Device{{Name: "Mac", Model: "MacBook Pro"}}, checkedAt); err != nil {
		t.Fatal(err)
	}
	if got := readDeviceCache(account); got == nil || len(got.Devices) != 1 || !got.CheckedAt.Equal(checkedAt) {
		t.Fatal("successful query was not retained")
	}
	info, err := os.Stat(filepath.Join(account.Dir, deviceCacheFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("device metadata must remain private")
	}
	for _, other := range []*Account{
		{ID: "02-account", AppleID: account.AppleID, Dir: account.Dir},
		{ID: account.ID, AppleID: "other@example.test", Dir: account.Dir},
	} {
		if readDeviceCache(other) != nil {
			t.Fatal("another account's devices were displayed")
		}
	}
}

func TestDeviceSnapshotPreservesCacheUntilSuccessfulQuery(t *testing.T) {
	dir := t.TempDir()
	account := &Account{ID: "01-account", AppleID: "owner@example.test", Dir: filepath.Join(dir, "account")}
	if err := os.MkdirAll(account.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveRegistry(dir, []*registryEntry{{ID: account.ID, AppleID: account.AppleID, Dir: account.Dir}}); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	server, err := New(Config{Dir: dir, Worker: executable, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.listener.Close()
	controller, err := control.New(account.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	checkedAt := time.Now().UTC().Add(-time.Minute)
	status := Status{PID: os.Getpid(), Running: true, Authenticated: true, State: "online", LastDeviceQueryAt: checkedAt, DeviceCount: 1}
	writeStatus := func() {
		t.Helper()
		data, _ := json.Marshal(status)
		if err := os.WriteFile(filepath.Join(account.Dir, "runtime.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeStatus()
	controller.SetDevices([]control.Device{{Name: "Mac"}})
	live := server.snapshotFor(context.Background(), account.ID)
	if len(live.Devices) != 1 || live.DevicesStale || readDeviceCache(account) == nil {
		t.Fatal("live list was not displayed and checkpointed")
	}

	// A restarted worker has no list. Its failed first query must expose the
	// reason while retaining the previous display instead of replacing it.
	controller.SetDevices(nil)
	status.LastDeviceQueryAt = time.Time{}
	status.DeviceCount = 0
	status.DeviceQueryStage = "device_identity_refresh_required"
	writeStatus()
	failed := server.snapshotFor(context.Background(), account.ID)
	if len(failed.Devices) != 1 || !failed.DevicesStale || failed.Status.DeviceQueryStage != status.DeviceQueryStage || !failed.DevicesCheckedAt.Equal(checkedAt) {
		t.Fatal("failed query hid the last successful result or failure reason")
	}

	// A real successful zero-device response replaces the old list.
	status.DeviceQueryStage = ""
	status.LastDeviceQueryAt = time.Now().UTC()
	writeStatus()
	empty := server.snapshotFor(context.Background(), account.ID)
	if len(empty.Devices) != 0 || empty.DevicesStale || empty.DevicesCheckedAt.IsZero() {
		t.Fatal("successful empty response was confused with a query failure")
	}
	if got := readDeviceCache(account); got == nil || len(got.Devices) != 0 {
		t.Fatal("obsolete devices survived a successful empty query")
	}

	controller.Close()
	stopped := server.snapshotFor(context.Background(), account.ID)
	if !stopped.DevicesStale || stopped.DevicesCheckedAt.IsZero() {
		t.Fatal("stopped account lost its saved device result")
	}
}
