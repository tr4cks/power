package ilo

import (
	"fmt"

	"github.com/tr4cks/power/modules"
)

type IloModule struct {
	modules.DefaultModule
	Config IloConfig
	Client *IloClient
}

type IloConfig struct {
	Hostname string `validate:"required"`
	Url      string `validate:"required"`
	Username string `validate:"required"`
	Password string `validate:"required"`
}

func New() modules.Module {
	return &IloModule{}
}

func (m *IloModule) Init(config map[string]interface{}) error {
	err := modules.Validate(config, &m.Config)
	if err != nil {
		return fmt.Errorf("error validating %q module configuration: %w", "ilo", err)
	}
	m.Client, err = NewClient(m.Config.Url, m.Config.Username, m.Config.Password)
	if err != nil {
		return fmt.Errorf("error creating ilo client: %w", err)
	}
	return nil
}

func (m *IloModule) State() (modules.Result[bool], modules.Result[bool]) {
	powerStateTask, powerStateChan := modules.MakeAsync(func() modules.Result[bool] {
		value, err := m.Client.PowerState()
		return modules.Result[bool]{*value == PowerStateOn, err}
	})

	pingTask, pingChan := modules.MakeAsync(func() modules.Result[bool] {
		value, err := modules.Ping(m.Config.Hostname)
		return modules.Result[bool]{value, err}
	})

	go powerStateTask()
	go pingTask()

	return <-powerStateChan, <-pingChan
}

func (m *IloModule) PowerOn() error {
	return m.Client.PushPowerButton()
}

func (m *IloModule) PowerOff() error {
	return m.Client.PushPowerButton()
}
