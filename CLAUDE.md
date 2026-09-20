# SMIConversionsVictor — grtv-recorder

Gravador de 8 canais de TV 24/7 em blocos de 4 minutos, serviço Windows em Go.
**A fonte de verdade é [`SPEC.md`](SPEC.md)** — leia-o antes de qualquer mudança de
comportamento; este arquivo é só um mapa de onde as coisas estão e o que não pode
quebrar.

O código do serviço vive em `grtv-recorder/` (não na raiz).

## Ambiente

- Este repositório **não tem Go instalado no PATH do sistema**. O toolchain usado
  para desenvolver está em `~/go-toolchain/go/bin` — adicione ao PATH antes de
  rodar `go`:
  ```bash
  export PATH=$HOME/go-toolchain/go/bin:$PATH
  ```
  Se essa pasta não existir mais numa sessão futura, baixe de https://go.dev/dl
  (foi usado go1.22.5 linux/amd64) ou peça ao usuário para instalar via apt
  (`golang-go`, requer sudo com senha).
- Não é um repositório git ainda (`git init` não foi rodado). Não assuma histórico.
- O entregável final só roda de fato como serviço no **Windows**; build e testes
  funcionam em Linux/macOS porque as partes que dependem de
  `golang.org/x/sys/windows/svc` ficam isoladas atrás de build tags
  (`internal/service/winservice_windows.go` vs `winservice_other.go`,
  `internal/channel/proc_windows.go` vs `proc_other.go`).

## Comandos

Rodar sempre a partir de `grtv-recorder/`:

```bash
go build ./...                                          # build linux (dev)
GOOS=windows GOARCH=amd64 go build -o grtv-recorder.exe ./cmd/recorder   # entregável
go vet ./...
go test ./...
```

## Layout (ver SPEC.md §12 para a intenção de cada arquivo)

```
grtv-recorder/
    cmd/recorder/main.go          entrypoint + subcomandos (install/uninstall/start/stop/status)
    internal/config/              carga + validação de config.yaml
    internal/service/             Recorder (orquestrador) + svc.Handler do Windows + status.json
    internal/channel/             supervisor de processo ffmpeg + watchdog (máquina de estados)
    internal/publish/             varredura, remux, thumbnail, nomeação/encosto na grade, mover
    internal/fsutil/              MkdirAll, rename atômico, sufixo anticolisão _1/_2
    internal/logging/             recorder.log, gaps.log, ffmpeg\<Canal>.log (lumberjack)
    scripts/install.bat|uninstall.bat   sc create/failure/depend (SPEC.md §10.2)
    config.yaml                   config real, copiado do SPEC.md §9
```

## Invariantes que NUNCA podem quebrar (motivo em SPEC.md)

Estas regras existem porque o sistema legado quebrou exatamente por violá-las.
Qualquer mudança que as contradiga é, quase certamente, um bug:

1. **`-c copy` sempre, nunca `-vcodec copy`** — reencode de áudio derruba o processo
   se o codec de origem não for aceito pelo encoder default (SPEC.md §5.3).
2. **`work_dir` e `output_dir` no mesmo volume** — a publicação depende de
   `os.Rename` ser atômico; `internal/config.Validate` rejeita configs que violem
   isso (SPEC.md §4 regra 2, §9).
3. **ffmpeg só escreve em `work\<Canal>\` (pasta plana, sem data)** — quem decide a
   pasta de data/canal final é o Go, lendo o nome do `.ts` (SPEC.md §4 regra 1).
4. **JPG é sempre movido antes do MP4** — nunca pode existir `.mp4` publicado sem
   `.jpg` ao lado (SPEC.md §7.2 passo 7, `internal/publish/publisher.go`).
5. **Pasta de destino = horário de INÍCIO do segmento**, nunca ajustado/arredondado
   — o bloco `23.56.00-00.00.00` fica no dia que termina (SPEC.md §6).
6. **`-segment_atclocktime 1`** é obrigatório — sem isso os blocos derivam do start
   do processo, não do relógio, e nunca mais alinham em 00:00/00:04/... (SPEC.md §5.3).
7. **Nunca sobrescrever** um par mp4/jpg já publicado — sempre sufixo `_1`, `_2`...
   e o mp4 e o jpg de um mesmo bloco compartilham o MESMO nome-base
   (`uniquePairBase` em `internal/publish/publisher.go`).
8. **Cada canal é independente** — falha/travamento de um não pode afetar os outros
   7 (SPEC.md §3). Qualquer refactor que introduza estado compartilhado entre
   workers de canais diferentes é suspeito.

## Testando o pipeline sem hardware real

Não há stream real nem ffmpeg de verdade neste ambiente de dev. Para validar
mudanças em `internal/publish` (remux/thumbnail/nomeação), a abordagem usada foi
criar scripts `ffmpeg`/`ffprobe` falsos (bash) que copiam o `.ts` de entrada para
o `.mp4` de saída e ecoam uma duração fixa, apontados via `config.yaml` de teste, e
rodar `Publisher.PublishAll()` num diretório temporário. Não deixe esses scripts
fake no repositório — são só para verificação pontual durante o desenvolvimento;
recrie-os quando precisar (veja o histórico de sessão para o formato exato, ou
recrie a partir de SPEC.md §7.2).

`internal/config` e `internal/publish` têm testes unitários reais (`go test
./...`) cobrindo validação de config e o cálculo de nomeação/encosto na grade
(incluindo virada de meia-noite) — mantenha-os passando.
