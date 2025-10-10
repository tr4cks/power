package modules

import (
	"fmt"
	"math"
	"net"
	"os"
	"strings"
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

func GenerateLogarithmicIntervals(timeout, minInterval, maxInterval time.Duration, curveShift, factor float64) ([]time.Duration, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be > 0")
	}
	if minInterval <= 0 {
		return nil, fmt.Errorf("minInterval must be > 0")
	}
	if maxInterval < minInterval {
		return nil, fmt.Errorf("maxInterval must be >= minInterval")
	}
	if curveShift <= 0 || math.IsNaN(curveShift) || math.IsInf(curveShift, 0) {
		return nil, fmt.Errorf("curveShift must be finite and > 0")
	}
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		factor = 1
	}

	minSec := minInterval.Seconds()
	maxSec := maxInterval.Seconds()
	total := timeout.Seconds()

	den := math.Log2(1 + total/curveShift)
	if den <= 0 || math.IsNaN(den) || math.IsInf(den, 0) {
		return []time.Duration{timeout}, nil
	}

	estimatedCapacity := int(math.Ceil(total/minSec)) + 1
	if estimatedCapacity < 0 {
		estimatedCapacity = 0
	}
	intervals := make([]time.Duration, 0, estimatedCapacity)

	const eps = 1e-9
	start := 0.0

	for start+eps < total {
		ratio := math.Log2(1+start/curveShift) / den
		if ratio < 0 {
			ratio = 0
		} else if ratio > 1 {
			ratio = 1
		}

		nextSec := (maxSec-minSec)*(1-ratio)*factor + minSec
		if nextSec < minSec {
			nextSec = minSec
		} else if nextSec > maxSec {
			nextSec = maxSec
		}

		remaining := total - start
		if nextSec > remaining {
			nextSec = remaining
		}
		if nextSec < eps {
			nextSec = math.Min(remaining, minSec)
			if nextSec < eps {
				break
			}
		}

		d := time.Duration(nextSec * float64(time.Second))
		if d <= 0 {
			d = time.Nanosecond
		}

		intervals = append(intervals, d)
		start += nextSec
	}

	return intervals, nil
}

func DisplayIntervals(intervals []time.Duration, maxInterval time.Duration) {
	fmt.Printf("%4s | %9s  | %11s  | %s\n", "Step", "Interval", "Cumulative", "Graphical")
	fmt.Printf("-----------------------------------%s\n", strings.Repeat("-", 80))

	var cumulative time.Duration
	step := 1
	maxBarLen := 80

	for _, interval := range intervals {
		cumulative += interval
		barLen := int(math.Round((float64(interval) / float64(maxInterval)) * float64(maxBarLen)))

		fmt.Printf(" %3d | %8.2fs  | %10.2fs  | %s\n",
			step, interval.Seconds(), cumulative.Seconds(), strings.Repeat(".", barLen))
		step++
	}
}

// Example usage
/*
func main() {
	timeout := 3 * time.Minute
	minInterval := 5 * time.Second
	maxInterval := 40 * time.Second
	curveShift := 1.5
	factor := 1.5

	intervals, err := modules.GenerateLogarithmicIntervals(timeout, minInterval, maxInterval, curveShift, factor)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	modules.DisplayIntervals(intervals, maxInterval)
}
*/
