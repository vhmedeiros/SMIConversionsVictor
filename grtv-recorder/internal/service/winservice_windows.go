//go:build windows

package service

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"grtv-recorder/internal/config"
	"grtv-recorder/internal/logging"
)

// IsWindowsService detecta se o processo foi iniciado pelo Service Control Manager
// (SPEC.md §10.1: "Detectar serviço vs. console com svc.IsWindowsService()").
func IsWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// winHandler implementa svc.Handler, respondendo a Interrogate/Stop/Shutdown
// (SPEC.md §10.1).
type winHandler struct {
	cfg    *config.Config
	logger *logging.Logger
	gaps   *logging.GapsLogger
}

func (h *winHandler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	rec := NewRecorder(h.cfg, h.logger, h.gaps)
	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()

	s <- svc.Status{State: svc.Running, Accepts: accepted}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				// WaitHint de 30s (SPEC.md §10.1).
				s <- svc.Status{State: svc.StopPending, WaitHint: 30000}
				cancel()
				<-done
				break loop
			}
		case <-done:
			// Recorder saiu por conta própria (não deveria acontecer em operação normal).
			break loop
		}
	}

	s <- svc.Status{State: svc.Stopped}
	return false, 0
}

// RunService entrega o controle ao Service Control Manager (bloqueia até o serviço parar).
func RunService(cfg *config.Config, logger *logging.Logger, gaps *logging.GapsLogger) error {
	return svc.Run(ServiceName, &winHandler{cfg: cfg, logger: logger, gaps: gaps})
}

// Install registra o serviço (start automático). Para failure actions e dependência
// de rede, ver scripts\install.bat (SPEC.md §10.2) — o subcomando cobre o registro básico.
func Install(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar ao Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(ServiceName); err == nil {
		existing.Close()
		return fmt.Errorf("serviço %s já está instalado", ServiceName)
	}

	s, err := m.CreateService(ServiceName, exePath, mgr.Config{
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return fmt.Errorf("criar serviço: %w", err)
	}
	defer s.Close()
	return nil
}

// Uninstall remove o serviço.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar ao Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("abrir serviço %s: %w", ServiceName, err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("remover serviço: %w", err)
	}
	return nil
}

// ControlStart pede ao SCM para iniciar o serviço já instalado.
func ControlStart() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar ao Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("abrir serviço %s: %w", ServiceName, err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("iniciar serviço: %w", err)
	}
	return nil
}

// ControlStop pede parada ao SCM e aguarda até 30s o serviço realmente parar.
func ControlStop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar ao Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("abrir serviço %s: %w", ServiceName, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("enviar Stop: %w", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for status.State != svc.Stopped {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout aguardando o serviço parar")
		}
		time.Sleep(500 * time.Millisecond)
		if status, err = s.Query(); err != nil {
			return fmt.Errorf("consultar status: %w", err)
		}
	}
	return nil
}
