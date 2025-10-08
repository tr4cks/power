package modules

import (
	"fmt"
	"net"
	"os"
	"syscall"
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

func isNoRouteOrDownError(err error) bool {
	opErr, ok := err.(*net.OpError)
	if !ok {
		return false
	}
	syscallErr, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		return false
	}
	return syscallErr.Err == syscall.EHOSTUNREACH || syscallErr.Err == syscall.EHOSTDOWN
}

func Ping(addr string) (bool, error) {
	pinger, err := probing.NewPinger(addr)
	if err != nil {
		return false, fmt.Errorf("error creating new pinger: %w", err)
	}
	pinger.Count = 1
	pinger.Timeout = 500 * time.Millisecond
	err = pinger.Run()
	if err != nil {
		if isNoRouteOrDownError(err) {
			return false, nil
		}
		return false, fmt.Errorf("error sending ping: %w", err)
	}
	stats := pinger.Statistics()
	return stats.PacketsRecv > 0, nil
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
