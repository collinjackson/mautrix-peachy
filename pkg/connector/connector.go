// Package connector is the bridgev2 network connector for Peachy.
package connector

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	up "go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/collinjackson/peachy-go"
)

//go:embed example-config.yaml
var ExampleConfig string

type Config struct {
	// The Peachy API.
	Server string `yaml:"server"`
	// Where each login's device keys and MLS state live, one folder per
	// login. Holds secrets: keep it private.
	DataDir string `yaml:"data_dir"`
	// What Peachy's Devices list calls the bridge's device. Applies to
	// logins made after it changes.
	DeviceName string `yaml:"device_name"`
	// Whether to send read receipts to Peachy when you read a chat in
	// Matrix. Unset follows your Peachy setting (Settings → Privacy).
	ReadReceipts *bool `yaml:"read_receipts"`
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "server")
	helper.Copy(up.Str, "data_dir")
	helper.Copy(up.Str, "device_name")
	helper.Copy(up.Bool|up.Null, "read_receipts")
}

type PeachyConnector struct {
	Bridge *bridgev2.Bridge
	Config Config
}

var _ bridgev2.NetworkConnector = (*PeachyConnector)(nil)

func (pc *PeachyConnector) Init(bridge *bridgev2.Bridge) {
	pc.Bridge = bridge
}

func (pc *PeachyConnector) Start(ctx context.Context) error {
	return os.MkdirAll(pc.Config.DataDir, 0o700)
}

func (pc *PeachyConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "Peachy",
		NetworkURL:           "https://pchy.app",
		NetworkID:            "peachy",
		BeeperBridgeType:     "github.com/collinjackson/mautrix-peachy",
		DefaultPort:          29350,
		DefaultCommandPrefix: "!peachy",
	}
}

func (pc *PeachyConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Message: func() any { return &MessageMetadata{} },
	}
}

func (pc *PeachyConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	// DisappearingMessages runs bridgev2's loop that redacts expired messages.
	return &bridgev2.NetworkGeneralCapabilities{DisappearingMessages: true}
}

func (pc *PeachyConnector) GetConfig() (string, any, up.Upgrader) {
	return ExampleConfig, &pc.Config, up.SimpleUpgrader(upgradeConfig)
}

func (pc *PeachyConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
}

// loginDir is where a finished login's device lives: named after the Peachy
// account's actor id, which is also the login id.
func (pc *PeachyConnector) loginDir(id networkid.UserLoginID) string {
	return filepath.Join(pc.Config.DataDir, string(id))
}

func (pc *PeachyConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	device, err := peachy.Open(pc.Config.Server, pc.loginDir(login.ID), pc.Config.DeviceName)
	if err != nil {
		return fmt.Errorf("open the Peachy device: %w", err)
	}
	login.Client = newClient(pc, login, device)
	return nil
}
