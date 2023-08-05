package wakeonlan

import (
	"fmt"

	"github.com/tr4cks/power/modules"

	"github.com/linde12/gowol"
)

type WakeOnLanModule struct {
	modules.DefaultModule
	Config WakeOnLanConfig
}

type WakeOnLanConfig struct {
	Hostname string `validate:"required"`
	Mac      string `validate:"required"`
}

func New() modules.Module {
	return &WakeOnLanModule{}
}

func (m *WakeOnLanModule) Init(config map[string]interface{}) error {
	err := modules.Validate(config, &m.Config)
	if err != nil {
		return fmt.Errorf("error validating %q module configuration: %w", "wol", err)
	}
	return nil
}

func (m *WakeOnLanModule) State() (modules.Result[bool], modules.Result[bool]) {
	ping, err := modules.Ping(m.Config.Hostname)
	return modules.Result[bool]{ping, err}, modules.Result[bool]{ping, err}
}

func (m *WakeOnLanModule) PowerOn() error {
	packet, err := gowol.NewMagicPacket(m.Config.Mac)
	if err != nil {
		return fmt.Errorf("error creating the magic packet: %w", err)
	}
	err = packet.Send("255.255.255.255")
	if err != nil {
		return fmt.Errorf("error sending the magic packet: %w", err)
	}
	return nil
}
