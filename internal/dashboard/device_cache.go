package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Laky-64/appleservices/internal/control"
)

const deviceCacheFile = "device-status.json"
const deviceCacheFreshFor = 10 * time.Minute

// Keep only the display fields, never Apple's raw response, device IDs or
// serial numbers. Bind both the workspace and its login account.
type deviceCacheRecord struct {
	Version   int              `json:"version"`
	AccountID string           `json:"account_id"`
	AppleID   string           `json:"apple_id"`
	CheckedAt time.Time        `json:"checked_at"`
	Devices   []control.Device `json:"devices"`
}

func validDeviceCache(record *deviceCacheRecord, account *Account) bool {
	if account == nil || account.AppleID == "" || record.Version != 1 || record.AccountID != account.ID ||
		!strings.EqualFold(record.AppleID, account.AppleID) || record.CheckedAt.IsZero() || record.CheckedAt.After(time.Now().Add(5*time.Minute)) || len(record.Devices) > 512 {
		return false
	}
	for _, device := range record.Devices {
		if len(device.Name) > 1024 || len(device.Model) > 1024 {
			return false
		}
	}
	return true
}

func writeDeviceCache(account *Account, devices []control.Device, checkedAt time.Time) error {
	if account == nil {
		return errors.New("missing device cache account")
	}
	if devices == nil {
		devices = []control.Device{}
	}
	record := deviceCacheRecord{Version: 1, AccountID: account.ID, AppleID: account.AppleID, CheckedAt: checkedAt, Devices: devices}
	if !validDeviceCache(&record, account) {
		return errors.New("invalid device cache binding")
	}
	return writeSafeCache(filepath.Join(account.Dir, deviceCacheFile), record)
}

func readDeviceCache(account *Account) *deviceCacheRecord {
	if account == nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(account.Dir, deviceCacheFile))
	if err != nil || len(data) > 128<<10 {
		return nil
	}
	var record deviceCacheRecord
	if json.Unmarshal(data, &record) != nil || !validDeviceCache(&record, account) {
		return nil
	}
	return &record
}
