//go:build !darwin && !linux && !windows

package processlock

import (
	"errors"
	"fmt"
)

var ErrBusy = errors.New("device store busy")

type Lock struct{}

func Acquire(string) (*Lock, error) { return nil, fmt.Errorf("Phase 1 supports macOS and Linux only") }
func (*Lock) Close() error          { return nil }
