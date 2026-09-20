//go:build !windows

// Stubs para compilar/testar fora do Windows (dev). O serviço real só existe no Windows.
package service

import (
	"errors"

	"grtv-recorder/internal/config"
	"grtv-recorder/internal/logging"
)

var errWindowsOnly = errors.New("disponível apenas no Windows")

func IsWindowsService() (bool, error) { return false, nil }

func RunService(_ *config.Config, _ *logging.Logger, _ *logging.GapsLogger) error {
	return errWindowsOnly
}

func Install(_ string) error { return errWindowsOnly }
func Uninstall() error       { return errWindowsOnly }
func ControlStart() error    { return errWindowsOnly }
func ControlStop() error     { return errWindowsOnly }
