package modules

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/mitchellh/mapstructure"
	probing "github.com/prometheus-community/pro-bing"
)

type Result[T any] struct {
	Value T
	Err   error
}

func MakeAsync[R any](routine func() R) (func(), chan R) {
	channel := make(chan R, 1)

	return func() {
		defer close(channel)
		channel <- routine()
	}, channel
}

func Ping(addr string) (bool, error) {
	pinger, err := probing.NewPinger(addr)
	if err != nil {
		return false, fmt.Errorf("error creating new pinger: %w", err)
	}
	pinger.Interval, err = time.ParseDuration("0.167s")
	if err != nil {
		return false, fmt.Errorf("error parsing duration: %w", err)
	}
	pinger.Timeout, err = time.ParseDuration("0.5s")
	if err != nil {
		return false, fmt.Errorf("error parsing duration: %w", err)
	}
	pinger.OnRecv = func(pkt *probing.Packet) {
		pinger.Stop()
	}
	err = pinger.Run()
	if err != nil {
		return false, fmt.Errorf("error sending ping: %w", err)
	}
	return pinger.PacketsRecv > 0, nil
}

func Validate[T any](input map[string]interface{}, output *T) error {
	err := mapstructure.Decode(input, output)
	if err != nil {
		return fmt.Errorf("input decoding error: %w", err)
	}
	validate := validator.New()
	err = validate.Struct(output)
	if err != nil {
		return fmt.Errorf("error validating structure fields: %w", err)
	}
	return nil
}
