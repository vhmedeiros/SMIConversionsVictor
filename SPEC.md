# SPEC — Gravador de Canais 24/7 (Windows Service, Go)

Documento de especificação para implementação. Escrito para ser lido por um agente de código.
Não há Docker. Instalação direta na máquina Windows.

---

## 1. Objetivo

Gravar **8 canais de TV simultaneamente**, 24 horas por dia, ininterruptamente, produzindo:

- um arquivo `.mp4` a cada **4 minutos**, alinhado ao relógio local (00:00, 00:04, 00:08 … 23:56);
- um `.jpg` com o **primeiro frame** de cada `.mp4`, mesmo nome, mesma pasta;
- organização em pastas por **data** e por **canal**.

O serviço deve sobreviver a: queda de stream, reinício da máquina, tela bloqueada, logoff do usuário.

### 1.1 Resultado esperado em disco

```
G:\Sistema\ftp\arquivos\stream\2026-09-18\Record\
    00.00.00-00.04.00.jpg
    00.00.00-00.04.00.mp4
    00.04.00-00.08.00.jpg
    00.04.00-00.08.00.mp4
    ...
    23.52.00-23.56.00.jpg
    23.52.00-23.56.00.mp4
    23.56.00-00.00.00.jpg
    23.56.00-00.00.00.mp4
```

360 pares de arquivos por canal por dia em operação normal.

---

## 2. Stack

| Item | Escolha | Motivo |
|---|---|---|
| Linguagem | **Go** (1.22+) | binário único, sem runtime a instalar; serviço Windows nativo via `golang.org/x/sys/windows/svc`; supervisão de processo trivial |
| Encoder | **ffmpeg** + **ffprobe** (build gyan.dev full, estático) | um processo ffmpeg por canal, 8 no total |
| Config | **YAML** (`gopkg.in/yaml.v3`) | editável sem recompilar |
| Log | arquivo rotativo (`gopkg.in/natefinch/lumberjack.v2`) | serviço não tem console |

Dependências Go:

```
golang.org/x/sys/windows/svc
gopkg.in/yaml.v3
gopkg.in/natefinch/lumberjack.v2
```

O ffmpeg **não** deve depender do `PATH`. O caminho vem do config.

---

## 3. Arquitetura

```
                 ┌──────────────────────────────┐
                 │  Windows Service (Go)        │
                 │  grtv-recorder.exe           │
                 └──────────────┬───────────────┘
                                │
          ┌──────────┬──────────┼──────────┬──────────┐
          │          │          │          │          │
     ┌────▼────┐ ┌───▼────┐ ┌───▼────┐   ...    ┌─────▼────┐
     │Channel  │ │Channel │ │Channel │          │ Channel  │
     │Worker   │ │Worker  │ │Worker  │          │ Worker   │
     │ Record  │ │ Globo  │ │TvAssem.│          │TvTropical│
     └────┬────┘ └───┬────┘ └───┬────┘          └─────┬────┘
          │          │          │                     │
   ┌──────▼───┐  ┌───▼──┐   ┌───▼──┐              ┌────▼─┐
   │ ffmpeg   │  │ffmpeg│   │ffmpeg│              │ffmpeg│   (processos filhos)
   └──────┬───┘  └───┬──┘   └───┬──┘              └────┬─┘
          │          │          │                     │
          └──────────┴─────┬────┴─────────────────────┘
                           │  escrevem .ts
                  ┌────────▼─────────┐
                  │  work\<Canal>\   │
                  └────────┬─────────┘
                           │
                  ┌────────▼─────────┐
                  │  Publisher       │  (1 goroutine por canal, ticker 5s)
                  │  remux + thumb   │
                  │  + rename        │
                  └────────┬─────────┘
                           │
                  ┌────────▼───────────────────────┐
                  │ arquivos\stream\<data>\<Canal>\│
                  └────────────────────────────────┘
```

Cada **Channel Worker** possui:
- um supervisor de processo ffmpeg (start / monitor / restart com backoff);
- um watchdog de produção (detecta stream travado);
- um publisher (varre a pasta de trabalho e publica o que estiver fechado).

Os 8 workers são independentes. Falha de um não afeta os demais.

---

## 4. Estrutura de diretórios

Duas árvores, **obrigatoriamente no mesmo volume**:

```
G:\Sistema\ftp\work\                 <- área de trabalho (não exposta ao FTP)
    Record\
        2026-09-18_00.00.00.ts       <- ffmpeg escreve aqui (pasta PLANA)
        2026-09-18_00.04.00.ts
    Globo\
    TvAssembleia\
    Band\
    Sbt\
    TvCidade\
    Cultura\
    TvTropical\
    _quarantine\                     <- arquivos inválidos, para inspeção manual

G:\Sistema\ftp\arquivos\stream\      <- árvore publicada (FTP lê daqui)
    2026-09-17\
        Record\
        Globo\
        ...
    2026-09-18\
        Record\
            00.00.00-00.04.00.mp4
            00.00.00-00.04.00.jpg
```

### Regras não negociáveis

1. **O ffmpeg nunca escreve na árvore publicada.** Ele só conhece `work\<Canal>\`, uma pasta plana, sem data no caminho. Quem decide a pasta de destino é o código Go, lendo a data do nome do `.ts`.
2. **`work` e `arquivos` no mesmo volume.** A publicação é um `os.Rename`, atômico e instantâneo no mesmo volume. Em volumes diferentes vira cópia de ~30 MB, não-atômica, e o FTP pode ler arquivo pela metade.
3. **Somente arquivo pronto entra na árvore publicada.** Nunca existe `.ts` nem `.mp4` parcial em `arquivos\stream\`.

---

## 5. Gravação (ffmpeg)

### 5.1 Comando — entrada HLS (`.m3u8`) — **padrão do projeto**

```
{ffmpeg} -nostdin -hide_banner -loglevel warning
  -reconnect 1
  -reconnect_streamed 1
  -reconnect_on_network_error 1
  -reconnect_delay_max 5
  -http_persistent 1
  -rw_timeout 15000000
  -i "http://192.168.129.228/8.m3u8"
  -map 0:v:0 -map 0:a:0?
  -c copy
  -f segment
  -segment_time 240
  -segment_atclocktime 1
  -reset_timestamps 1
  -segment_format mpegts
  -strftime 1
  "G:\Sistema\ftp\work\TvTropical\%Y-%m-%d_%H.%M.%S.ts"
```

### 5.2 Comando — entrada RTSP (fallback)

Trocar o bloco de entrada por:

```
  -rtsp_transport tcp
  -timeout 15000000          # em builds ffmpeg < 6.0 o nome é -stimeout
  -i "rtsp://192.168.1.168/8"
```

O resto é idêntico. O supervisor monta o bloco de entrada conforme o esquema da URL (`http://`/`https://` → HLS, `rtsp://` → RTSP), permitindo misturar canais das duas formas no mesmo config.

### 5.3 Justificativa de cada flag

| Flag | Por quê |
|---|---|
| `-c copy` | **Nunca** usar `-vcodec copy`. `-vcodec copy` copia só o vídeo e **reencoda o áudio** no codec default do container; se o áudio da fonte não for aceito, o processo morre. Esta é a causa mais provável do sistema antigo ter parado. |
| `-segment_atclocktime 1` | Sem isso, o corte de 240s é contado a partir do **start do processo**, não do relógio. Os blocos derivam de 00:00/00:04 e nunca mais alinham. Flag ausente no sistema antigo. |
| `-segment_format mpegts` | MPEG-TS tolera truncamento. Em queda de energia o último arquivo ainda abre. Com `mp4` direto, o arquivo aberto no momento da queda fica sem `moov` atom e é **perda total** — problema do sistema antigo. |
| `-reconnect*` | Sem elas, qualquer soluço de rede mata o ffmpeg. Flags ausentes no sistema antigo. |
| `-map 0:v:0 -map 0:a:0?` | Pega exatamente 1 vídeo + 1 áudio. O `?` torna o áudio opcional (canal sem áudio não derruba o processo). Evita arrastar streams de dados/legenda que quebram o mp4. |
| `-strftime 1` | Habilita `%Y-%m-%d_%H.%M.%S` no nome de saída. |
| `-nostdin` | Serviço não tem stdin; sem isso o ffmpeg pode travar. |

### 5.4 Precisão do corte

- **RTSP:** corta no keyframe mais próximo da borda → desvio de ±1 GOP (tipicamente < 2s).
- **HLS:** corta na borda do segmento da playlist → desvio de ±1 segmento HLS (2 a 10s).

O **nome** do arquivo sai sempre na grade certa (ver §7.2). O **conteúdo** pode ter alguns segundos de folga na emenda. Se precisar de precisão maior, usar RTSP.

`240s` divide a hora em 15 blocos exatos, portanto a grade fecha em 23:56 e a virada de meia-noite é uma borda como qualquer outra.

---

## 6. Virada de dia — comportamento detalhado

**Nada reinicia à meia-noite.** O processo ffmpeg fica vivo por dias. Meia-noite é um segmento comum.

```
23:52:00  ffmpeg abre  work\Globo\2026-09-17_23.52.00.ts
          publisher publica  ...\2026-09-17\Globo\23.48.00-23.52.00.mp4

23:56:00  ffmpeg abre  work\Globo\2026-09-17_23.56.00.ts
          publisher publica  ...\2026-09-17\Globo\23.52.00-23.56.00.mp4

00:00:00  ffmpeg abre  work\Globo\2026-09-18_00.00.00.ts    <- MESMO processo
          publisher publica  ...\2026-09-17\Globo\23.56.00-00.00.00.mp4
                                        ^^^^^^^^^^ último arquivo do dia 17

00:04:00  ffmpeg abre  work\Globo\2026-09-18_00.04.00.ts
          publisher: MkdirAll ...\2026-09-18\Globo\
          publica  ...\2026-09-18\Globo\00.00.00-00.04.00.mp4
                                        ^^^^^^^^^^ primeiro arquivo do dia 18
```

### Regra de destino

> A pasta de destino é determinada **exclusivamente pelo horário de INÍCIO** do segmento.

Consequência: o bloco 23:56→00:00 fica na pasta do **dia anterior**, com o nome `23.56.00-00.00.00.mp4`. Isso é intencional e replica o comportamento do sistema legado.

O horário de início **nunca** é ajustado ou arredondado — ele define a pasta, e mexer nele poderia empurrar um arquivo de 23:59:58 para a pasta errada.

### 6.1 Pré-criação de pastas (`pre_create_day_folders: true`)

Como a pasta do dia só nasceria às 00:04 (quando o primeiro arquivo é publicado), uma goroutine cria às **00:00:05** as 8 pastas vazias do dia corrente:

```
arquivos\stream\<hoje>\Record\
arquivos\stream\<hoje>\Globo\
... (8 canais)
```

Isso replica o comportamento legado (pasta existe desde 00:00). Também executa no arranque do serviço, para o dia corrente. Configurável.

---

## 7. Publicação

Uma goroutine por canal, ticker de **5 segundos**, varrendo `work\<Canal>\*.ts`.

### 7.1 Detecção de arquivo fechado

Um `.ts` é considerado **fechado** se:

- existe outro `.ts` **mais recente** na mesma pasta (caso normal: o ffmpeg abre o próximo no mesmo instante em que fecha o anterior); **OU**
- o `mtime` não muda há mais de **300 segundos** (caso pós-crash: `.ts` órfão deixado por reboot ou queda de energia).

A segunda condição é o que garante que nenhum arquivo é perdido em reinício. Como o formato é MPEG-TS, o órfão truncado ainda é válido e perde no máximo os últimos segundos.

### 7.2 Pipeline (por arquivo, dentro de `work\<Canal>\`)

```
1. VALIDAR
   ffprobe -v error -show_entries format=duration \
           -of default=noprint_wrappers=1:nokey=1 <arquivo>.ts
   → duração ausente ou < 1.0s  ⇒ mover para work\_quarantine\, logar WARN, abortar

2. REMUX  (sem reencode, custo de CPU ~0)
   {ffmpeg} -nostdin -y -hide_banner -loglevel error
     -i <arquivo>.ts
     -map 0:v:0 -map 0:a:0?
     -c copy -movflags +faststart
     <arquivo>.mp4

3. DURAÇÃO REAL
   ffprobe no .mp4 → duration (segundos, float)

4. THUMBNAIL (extraída do .mp4 final, não do .ts)
   {ffmpeg} -nostdin -y -hide_banner -loglevel error
     -i <arquivo>.mp4
     -frames:v 1 -q:v 3 -an
     <arquivo>.jpg

5. NOMEAR
   inicio = parse do nome do .ts  ("2026-09-18_00.00.00" → 2026-09-18 00:00:00)
   fim    = inicio + round(duration)
   se |fim - borda_da_grade_mais_proxima| <= 3s: fim = borda_da_grade
   nome   = fmt("%02d.%02d.%02d-%02d.%02d.%02d", inicio..., fim...)

6. DESTINO
   dst = arquivos\stream\<inicio.yyyy-MM-dd>\<Canal>\
   MkdirAll(dst)
   se dst\<nome>.mp4 já existe: sufixar "_1", "_2", ... (NUNCA sobrescrever)

7. MOVER  (ordem obrigatória: JPG primeiro)
   os.Rename(<arquivo>.jpg, dst\<nome>.jpg)
   os.Rename(<arquivo>.mp4, dst\<nome>.mp4)

8. LIMPAR
   os.Remove(<arquivo>.ts)
```

**JPG antes do MP4** para que nunca exista um `.mp4` visível no FTP sem a miniatura ao lado. Se qualquer passo falhar, o `.ts` permanece em `work\` e será retentado no próximo tick (máx. 3 tentativas, depois vai para `_quarantine`).

### 7.3 Encosto na grade (passo 5)

- duração real 239,96s → fim seria `00.03.59` → encosta → **`00.04.00`** ✔
- duração real 180s (stream caiu) → fim `11.07.00`, fora da tolerância → mantém → **`11.04.00-11.07.00.mp4`** ✔

Arquivos irregulares são esperados e legítimos: sinalizam interrupção de stream.

---

## 8. Watchdog

Máquina de estados por canal:

```
STOPPED ──start──> RUNNING ──processo morreu──> BACKOFF ──timer──> RUNNING
                      │                            ▲
                      └──sem .ts novo > 300s────────┘
                         (kill + restart)
```

| Gatilho | Ação |
|---|---|
| Processo ffmpeg terminou (qualquer exit code) | reinicia com backoff exponencial: 2s, 4s, 8s, 16s, 32s, 60s (teto). Reset do backoff após 120s de execução estável. |
| Processo vivo, mas **nenhum `.ts` novo** criado há > 300s | `Kill()` no processo e reinicia. Cobre o caso de stream travado que não fecha a conexão TCP — comum em encoders IP. |
| Serviço recebe SIGTERM / `svc.Stop` | envia `q` no stdin do ffmpeg (shutdown limpo), aguarda até 10s, depois `Kill()`. Publica o que estiver fechado antes de sair. |

### 8.1 Registro de falhas

Toda interrupção gera uma linha em `logs\gaps.log`:

```
2026-09-18T11:07:14-04:00  Sbt  GAP  inicio=11:07:03 fim=11:12:00 duracao=297s motivo=process_exit code=1
```

Permite auditar depois por que faltou arquivo em determinado intervalo.

---

## 9. Configuração

`config.yaml`, ao lado do executável.

```yaml
# Caminhos
ffmpeg_path:  "C:\\Sistema\\bin\\ffmpeg.exe"
ffprobe_path: "C:\\Sistema\\bin\\ffprobe.exe"
work_dir:     "G:\\Sistema\\ftp\\work"
output_dir:   "G:\\Sistema\\ftp\\arquivos\\stream"
log_dir:      "G:\\Sistema\\ftp\\logs"

# Segmentação
segment_seconds: 240          # 4 minutos; deve dividir 3600
grid_snap_seconds: 3          # tolerância de encosto na grade (§7.3)

# Watchdog
stall_timeout_seconds: 300    # sem .ts novo por este tempo = travado
backoff_initial_seconds: 2
backoff_max_seconds: 60
orphan_timeout_seconds: 300   # mtime parado = .ts órfão publicável

# Publisher
publish_tick_seconds: 5
max_publish_retries: 3

# Comportamento
pre_create_day_folders: true  # cria pastas do dia às 00:00:05
log_level: "info"             # debug | info | warn | error

channels:
  - name: "Record"
    label: "TV IMPERIAL - AF. TV RECORD - RR"
    url: "http://192.168.129.228/0.m3u8"
    enabled: true

  - name: "Globo"
    label: "TV RORAIMA - AF. GLOBO - RR"
    url: "http://192.168.129.228/2.m3u8"
    enabled: true

  - name: "TvAssembleia"
    label: "TV ASSEMBLEIA - RR"
    url: "http://192.168.129.228/4.m3u8"
    enabled: true

  - name: "Band"
    label: "TV BAND RORAIMA - RR"
    url: "http://192.168.129.228/6.m3u8"
    enabled: true

  - name: "Sbt"
    label: "TV NORTE / AF. SBT - RR"
    url: "http://192.168.129.228/14.m3u8"
    enabled: true

  - name: "TvCidade"
    label: "TV CIDADE - RR"
    url: "http://192.168.129.228/12.m3u8"
    enabled: true

  - name: "Cultura"
    label: "TV ATIVA RORAIMA"
    url: "http://192.168.129.228/10.m3u8"
    enabled: true

  - name: "TvTropical"
    label: "TV TROPICAL RR"
    url: "http://192.168.129.228/8.m3u8"
    enabled: true
```

### Notas sobre o mapeamento

- `name` é o **nome da pasta** — não pode conter `\ / : * ? " < > |`, e é validado no arranque.
- `label` é apenas descritivo (aparece em log e no futuro endpoint de status).
- O canal `Sbt` usa o índice `14` e `TvCidade` usa `12` — **não** seguem a sequência par de 0 a 12. Manter exatamente como acima.
- Equivalência RTSP ↔ HLS: `rtsp://192.168.1.168/<N>` ≡ `http://192.168.129.228/<N>.m3u8`. Basta trocar a `url` para alternar.
- Validação no arranque: `segment_seconds` deve dividir 3600; nomes de canal únicos; URLs não vazias; `work_dir` e `output_dir` no mesmo volume (senão, erro fatal com mensagem explícita).

---

## 10. Serviço Windows

### 10.1 Implementação

Usar `golang.org/x/sys/windows/svc`. O `main()` detecta o modo:

```
grtv-recorder.exe                 → roda em foreground (desenvolvimento/teste)
grtv-recorder.exe install         → registra o serviço
grtv-recorder.exe uninstall       → remove o serviço
grtv-recorder.exe start | stop    → controla
grtv-recorder.exe status          → estado dos 8 canais
```

Detectar serviço vs. console com `svc.IsWindowsService()`.

Handler deve responder a `svc.Interrogate`, `svc.Stop` e `svc.Shutdown`. No `Stop`/`Shutdown`, iniciar o desligamento limpo (§8) e pedir `SERVICE_STOP_PENDING` com `WaitHint` de 30s.

### 10.2 Registro e recuperação

```bat
sc create GRTVRecorder ^
   binPath= "C:\Sistema\bin\grtv-recorder.exe" ^
   start= auto ^
   DisplayName= "GRTV Recorder"

sc description GRTVRecorder "Gravacao continua de 8 canais de TV em blocos de 4 minutos"

REM Reinicia sozinho em caso de falha: 10s, 30s, 60s; contador zera a cada 24h
sc failure GRTVRecorder reset= 86400 actions= restart/10000/restart/30000/restart/60000

REM Aguarda a rede estar pronta antes de subir (evita falha em boot)
sc config GRTVRecorder depend= Tcpip/Dnscache
```

Alternativa a `start= auto`: `start= delayed-auto`, que dá margem para a interface de rede e o volume G: ficarem prontos. **Recomendado.**

### 10.3 Conta de execução

Rodar como `LocalSystem` (default) **só funciona se `G:` for um volume local**. Se `G:` for unidade de rede mapeada, `LocalSystem` não enxerga o mapeamento — nesse caso:

- usar caminho UNC (`\\servidor\share\...`) no config; **e**
- rodar o serviço sob uma conta de domínio/local com permissão de escrita no share (`sc config GRTVRecorder obj= .\usuario password= senha`).

Isso deve ser verificado antes da instalação.

### 10.4 Requisitos de ambiente

- Fuso horário do Windows: **(UTC-04:00) Boa Vista** (Roraima é UTC-4 e não observa horário de verão). Fuso errado desloca todos os nomes de arquivo.
- Sincronização NTP ativa (`w32tm`). Ajustes grandes de relógio para trás podem gerar colisão de nome — tratada pelo sufixo `_1`, mas devem ser raros.
- Energia: desativar suspensão e hibernação (`powercfg /change standby-timeout-ac 0`, `powercfg /hibernate off`).
- Excluir `work\` e `arquivos\stream\` da verificação em tempo real do antivírus (I/O intenso e contínuo).

---

## 11. Logging

```
logs\
    recorder.log       principal, rotativo (50 MB, 10 arquivos, compressão)
    gaps.log           interrupções (§8.1)
    ffmpeg\<Canal>.log stderr do ffmpeg, rotativo (10 MB, 3 arquivos)
```

Formato: `2026-09-18T00:04:03-04:00  INFO  [Globo]  mensagem  campo=valor`

Eventos obrigatórios em nível INFO:
- arranque do serviço, com resumo da config validada;
- start/restart de cada ffmpeg, com o comando completo;
- cada publicação bem-sucedida (`published file=00.00.00-00.04.00.mp4 dur=240.0s`);
- criação de pasta de dia.

Nível WARN: quarentena, retry de publicação, `.ts` órfão recolhido.
Nível ERROR: falha de ffprobe/remux após todos os retries, falha de rename, config inválida.

---

## 12. Layout do projeto

```
grtv-recorder\
    go.mod
    config.yaml
    cmd\recorder\main.go          entrypoint, modo serviço vs console, subcomandos
    internal\
        config\config.go          load + validação (mesmo volume, 3600 % seg == 0, nomes)
        service\service.go        svc.Handler, ciclo de vida
        channel\worker.go         supervisor + máquina de estados do watchdog
        channel\ffmpeg.go         montagem do comando (HLS vs RTSP), start, kill
        publish\publisher.go      varredura, detecção de fechado, pipeline
        publish\naming.go         parse do início, cálculo do fim, encosto na grade
        publish\probe.go          wrapper de ffprobe
        fsutil\fsutil.go          MkdirAll, rename atômico, sufixo anticolisão
        logging\logging.go        lumberjack + níveis
    scripts\
        install.bat               sc create + failure + depend
        uninstall.bat
    README.md
```

---

## 13. Critérios de aceite

Implementação considerada completa quando **todos** forem verificáveis:

1. **Grade** — rodando 1 hora com os 8 canais, cada pasta contém 15 pares `.mp4`/`.jpg` com horários exatos `HH.00.00`, `HH.04.00`, … `HH.56.00`.
2. **Pareamento** — nenhum `.mp4` sem `.jpg` correspondente, e vice-versa.
3. **Primeiro frame** — o `.jpg` corresponde visualmente ao frame inicial do `.mp4` ao lado.
4. **Integridade** — todo `.mp4` publicado abre em VLC e reporta duração via ffprobe; nenhum arquivo com `moov` ausente.
5. **Áudio preservado** — ffprobe mostra stream de áudio no `.mp4` final com o **mesmo codec** da fonte (prova de que não houve reencode).
6. **Virada de dia** — o arquivo `23.56.00-00.00.00.mp4` existe na pasta do dia que termina, e `00.00.00-00.04.00.mp4` na pasta do dia que começa. Nenhuma lacuna entre os dois.
7. **Pasta pré-criada** — às 00:00:05 as 8 pastas do novo dia já existem (com `pre_create_day_folders: true`).
8. **Resiliência de rede** — derrubando a fonte de um canal por 3 minutos: o canal volta sozinho; os demais 7 continuam sem qualquer interrupção; a lacuna aparece em `gaps.log`.
9. **Resiliência de reboot** — reiniciando a máquina no meio de um segmento: o serviço sobe sozinho, o `.ts` órfão é recolhido e publicado com nome irregular (ex.: `14.04.00-14.06.00.mp4`), e a gravação retoma na grade.
10. **Tela bloqueada** — com a sessão bloqueada e sem usuário logado, a gravação continua normalmente por pelo menos 30 minutos.
11. **Isolamento** — `arquivos\stream\` nunca contém `.ts` nem arquivo de tamanho crescente; verificável com varredura contínua durante 1 hora.
12. **Carga** — com 8 canais, uso de CPU do conjunto abaixo de 15% em máquina modesta (prova de que não há reencode em lugar nenhum).

---

## 14. Fora de escopo

- Retenção/limpeza de arquivos antigos (tratada por outro sistema).
- Servidor FTP (já existe).
- Interface web / API de status (pode entrar em versão futura; o subcomando `status` cobre a necessidade imediata).
- Reencode de fontes que não sejam H.264/AAC. Se algum canal vier em MPEG-2 ou outro codec incompatível com MP4, isso deve ser detectado no arranque e **logado como ERROR**, não corrigido silenciosamente.