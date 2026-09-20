// Comando grtv-recorder: gravador de 8 canais de TV 24/7 (SPEC.md).
//
//	grtv-recorder.exe                 roda em foreground (desenvolvimento/teste)
//	grtv-recorder.exe install         registra o serviço
//	grtv-recorder.exe uninstall       remove o serviço
//	grtv-recorder.exe start | stop    controla
//	grtv-recorder.exe status          estado dos 8 canais
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"grtv-recorder/internal/config"
	"grtv-recorder/internal/logging"
	"grtv-recorder/internal/service"
)

func main() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao localizar o executável:", err)
		os.Exit(1)
	}
	// config.yaml ao lado do executável (SPEC.md §9).
	configPath := filepath.Join(filepath.Dir(exePath), "config.yaml")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			runInstall(exePath)
		case "uninstall":
			runUninstall()
		case "start":
			runStart()
		case "stop":
			runStop()
		case "status":
			runStatus(configPath)
		case "-h", "--help", "help":
			printUsage()
		default:
			fmt.Fprintf(os.Stderr, "comando desconhecido: %s\n\n", os.Args[1])
			printUsage()
			os.Exit(2)
		}
		return
	}

	runRecorder(configPath)
}

func printUsage() {
	fmt.Println(`uso:
  grtv-recorder.exe                 roda em foreground (desenvolvimento/teste)
  grtv-recorder.exe install         registra o serviço
  grtv-recorder.exe uninstall       remove o serviço
  grtv-recorder.exe start | stop    controla
  grtv-recorder.exe status          estado dos 8 canais`)
}

func loadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao carregar config:", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config inválida:", err)
		os.Exit(1)
	}
	return cfg
}

func runInstall(exePath string) {
	if err := service.Install(exePath); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("serviço instalado. Rode scripts\\install.bat para configurar failure actions e dependência de rede (SPEC.md §10.2).")
}

func runUninstall() {
	if err := service.Uninstall(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("serviço removido.")
}

func runStart() {
	if err := service.ControlStart(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("serviço iniciado.")
}

func runStop() {
	if err := service.ControlStop(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("serviço parado.")
}

func runStatus(configPath string) {
	cfg := loadConfig(configPath)
	st, err := service.ReadStatus(cfg.LogDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao ler status (o serviço está rodando?):", err)
		os.Exit(1)
	}

	fmt.Printf("atualizado em: %s\n", st.UpdatedAt.Format(time.RFC3339))
	for _, c := range st.Channels {
		line := fmt.Sprintf("  %-15s %-8s", c.Name, c.State)
		if c.LastError != "" {
			line += " err=" + c.LastError
		}
		fmt.Println(line)
	}
}

func runRecorder(configPath string) {
	cfg := loadConfig(configPath)

	level := logging.ParseLevel(cfg.LogLevel)
	logger := logging.New(cfg.LogDir, level)
	defer logger.Close()
	gaps := logging.NewGapsLogger(cfg.LogDir)
	defer gaps.Close()

	isService, err := service.IsWindowsService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao detectar modo serviço:", err)
		os.Exit(1)
	}

	if isService {
		if err := service.RunService(cfg, logger, gaps); err != nil {
			logger.Error("-", "falha fatal do serviço", "err", err)
			os.Exit(1)
		}
		return
	}

	// Modo console (dev/teste): roda em foreground até Ctrl+C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rec := service.NewRecorder(cfg, logger, gaps)
	rec.Run(ctx)
}
