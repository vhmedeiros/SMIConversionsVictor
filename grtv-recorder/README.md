# grtv-recorder

Gravador de 8 canais de TV 24/7 em blocos de 4 minutos, como serviço Windows.
Especificação completa em [`../SPEC.md`](../SPEC.md).

## Build

Cross-compile a partir de Linux/macOS/Windows (não precisa de CGO):

```bash
GOOS=windows GOARCH=amd64 go build -o grtv-recorder.exe ./cmd/recorder
```

## Uso

```
grtv-recorder.exe                 roda em foreground (desenvolvimento/teste)
grtv-recorder.exe install         registra o serviço no Windows
grtv-recorder.exe uninstall       remove o serviço
grtv-recorder.exe start | stop    controla o serviço
grtv-recorder.exe status          estado dos 8 canais (lê logs\status.json)
```

`config.yaml` deve estar ao lado do `.exe`. Veja `config.yaml` neste diretório
para o formato completo (caminhos do ffmpeg/ffprobe, work_dir/output_dir,
segmentação, watchdog, publisher e lista de canais).

Depois de `install`, rode `scripts\install.bat` como Administrador para configurar
failure actions (restart automático) e dependência de rede — isso não é feito pelo
subcomando `install` sozinho (SPEC.md §10.2).

## Estrutura

```
cmd/recorder/main.go          entrypoint, subcomandos, modo serviço vs console
internal/config/              carga + validação do config.yaml
internal/service/             orquestrador (Recorder) + svc.Handler do Windows
internal/channel/             supervisor de processo ffmpeg + watchdog por canal
internal/publish/             varredura, remux, thumbnail, nomeação, publicação
internal/fsutil/               MkdirAll, rename atômico, sufixo anticolisão
internal/logging/             recorder.log, gaps.log, ffmpeg\<Canal>.log
```

## Testando localmente (sem Windows)

O binário só roda como serviço de fato no Windows, mas o build e os testes de
unidade (`internal/config`, `internal/publish`) funcionam em qualquer SO — as
partes que dependem de `golang.org/x/sys/windows/svc` ficam isoladas atrás de
build tags (`_windows.go` / `_other.go`).

```bash
go build ./...
go vet ./...
go test ./...
```

Para rodar de verdade em foreground (Linux/macOS, com ffmpeg local só para
testar o pipeline de publicação — a gravação HLS/RTSP funciona igual, o que
muda é só o registro como serviço):

```bash
go run ./cmd/recorder
```
