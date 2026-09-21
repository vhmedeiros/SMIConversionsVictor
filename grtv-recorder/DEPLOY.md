# Deploy e primeiro teste — Windows 11 "do zero"

Passo a passo para preparar uma máquina Windows 11 nova e rodar o `grtv-recorder`
nela pela primeira vez, em console (sem instalar como serviço ainda), antes de
promovê-lo a serviço de verdade. Ver `SPEC.md` para a especificação completa.

## 1. Preparar a máquina

Rodar como Administrador (PowerShell).

1. **Fuso horário** — Configurações → Hora e idioma → Fuso horário =
   `(UTC-04:00) Boa Vista`, e desativar "ajustar automaticamente para o horário de
   verão" (Roraima não observa). Confirmar NTP ativo:
   ```powershell
   w32tm /query /status
   ```
2. **Desativar suspensão/hibernação**:
   ```powershell
   powercfg /change standby-timeout-ac 0
   powercfg /change standby-timeout-dc 0
   powercfg /hibernate off
   ```
3. **Decidir a letra de unidade** que vai guardar `work\` e `arquivos\stream\`.
   O `config.yaml` de exemplo assume `G:\`, herdado do sistema legado (que lê o FTP
   dali). Se esta máquina nova não tiver esse `G:\` (disco/mapeamento já pronto),
   escolha outra unidade — **mas `work_dir` e `output_dir` precisam estar no MESMO
   volume**, senão o `grtv-recorder` recusa subir (validação de config).
4. **Criar as pastas** (ajuste a letra conforme o passo 3):
   ```powershell
   New-Item -ItemType Directory -Force C:\Sistema\bin
   New-Item -ItemType Directory -Force G:\Sistema\ftp\work
   New-Item -ItemType Directory -Force G:\Sistema\ftp\arquivos\stream
   New-Item -ItemType Directory -Force G:\Sistema\ftp\logs
   ```
5. **Excluir do antivírus** (Windows Defender) as pastas de I/O intenso:
   ```powershell
   Add-MpPreference -ExclusionPath "G:\Sistema\ftp\work"
   Add-MpPreference -ExclusionPath "G:\Sistema\ftp\arquivos"
   ```

## 2. Instalar o ffmpeg

1. Baixar o build estático **full** de https://www.gyan.dev/ffmpeg/builds/
   (`ffmpeg-release-full.7z`).
2. Extrair e copiar `bin\ffmpeg.exe` e `bin\ffprobe.exe` para `C:\Sistema\bin\`.
3. Testar:
   ```powershell
   C:\Sistema\bin\ffmpeg.exe -version
   C:\Sistema\bin\ffprobe.exe -version
   ```

## 3. Levar o binário para a máquina

O `.exe` é compilado por cross-compilation (não precisa de Go instalado no
Windows). A partir do checkout do repositório:

```bash
GOOS=windows GOARCH=amd64 go build -o grtv-recorder.exe ./cmd/recorder
```

Transferir `grtv-recorder.exe` para `C:\Sistema\bin\` na máquina de destino
(USB, compartilhamento de rede, download de uma Release do GitHub, etc.).

## 4. Configurar `config.yaml`

Copiar `grtv-recorder/config.yaml` do repositório para `C:\Sistema\bin\config.yaml`
(**ao lado do `.exe`** — é onde o programa procura por padrão) e ajustar:

- `work_dir`, `output_dir`, `log_dir` com a letra de unidade decidida no passo 1.3;
- `ffmpeg_path` / `ffprobe_path` apontando para `C:\Sistema\bin\ffmpeg.exe` /
  `ffprobe.exe`;
- as URLs dos 8 canais — as do exemplo são as de produção
  (`http://192.168.129.228/...`); se o teste for numa rede diferente, ajuste ou
  desabilite (`enabled: false`) os canais que ainda não são alcançáveis dali, para
  testar com 1–2 canais primeiro.

Dica para o primeiro teste: reduzir `segment_seconds` para algo como `60` (tem que
dividir 3600) encurta o ciclo de espera para ver o primeiro arquivo publicado.
Voltar para `240` antes de ir para produção.

## 5. Primeiro teste — modo console

Nada precisa ser instalado ainda. Abrir PowerShell/cmd como administrador:

```powershell
cd C:\Sistema\bin
.\grtv-recorder.exe
```

Em modo console (fora do SCM) o log aparece tanto na tela quanto em
`logs\recorder.log` — não precisa abrir outro terminal para acompanhar.

O que observar, na ordem:

1. Linha de arranque: `arranque do serviço  canais=N segment_seconds=...`.
2. Uma linha `iniciando ffmpeg` por canal habilitado, com o comando completo.
3. Dentro de `work\<Canal>\`, o `.ts` sendo escrito (cresce em tamanho).
4. Depois de um ciclo de `segment_seconds`, o `.ts` fecha e, no próximo tick do
   publisher (até 5s depois), aparece `published file=... dur=...` no log.
5. Em `arquivos\stream\<hoje>\<Canal>\`, o par `.mp4`+`.jpg` com o nome
   `HH.MM.SS-HH.MM.SS`.

Para parar: `Ctrl+C`. Deve aparecer `parando ffmpeg (shutdown limpo)` no log, e o
segmento em andamento deve ser fechado e publicado antes do processo sair.

### Conferir os arquivos publicados

```powershell
& C:\Sistema\bin\ffprobe.exe -v error -show_entries stream=codec_type,codec_name -of default=noprint_wrappers=1 "G:\Sistema\ftp\arquivos\stream\<data>\<Canal>\<arquivo>.mp4"
```

Deve listar um stream de vídeo e um de áudio, com o **mesmo codec da fonte**
(prova de que não houve reencode — SPEC.md §13 critério 5). Abrir o `.mp4` no VLC
para conferir imagem/áudio, e comparar visualmente o `.jpg` com o primeiro frame.

## 6. Instalar como serviço

Só depois do teste em console ter funcionado:

```powershell
cd C:\Sistema\bin
.\grtv-recorder.exe install
```

Depois, como Administrador, rodar `scripts\install.bat` (ajustar a variável
`BIN` no início do arquivo se o caminho não for `C:\Sistema\bin\grtv-recorder.exe`)
— ele configura failure actions (restart automático) e dependência da rede
(SPEC.md §10.2), o que o subcomando `install` sozinho não faz.

Iniciar:
```powershell
.\grtv-recorder.exe start
```

## 7. Verificar o serviço rodando

```powershell
.\grtv-recorder.exe status
```
Lê `logs\status.json`, atualizado a cada 5s pelo próprio serviço — mostra o
estado (`RUNNING`/`BACKOFF`/`STOPPED`) de cada canal.

Também é possível checar via `services.msc`: o serviço aparece como
**GRTV Recorder**, Startup Type = *Automatic (Delayed Start)*.

## 8. Testes de resiliência (SPEC.md §13)

Com o serviço já rodando de forma estável:

- **Queda de rede de 1 canal por ~3 min**: os outros 7 não podem parar; o canal
  afetado deve voltar sozinho e gerar uma linha em `logs\gaps.log`.
- **Reboot no meio de uma gravação**: o serviço deve subir sozinho (start
  automático); o `.ts` órfão deixado pra trás deve ser publicado com nome
  irregular (ex.: `14.04.00-14.06.00.mp4`) assim que o `orphan_timeout_seconds`
  (padrão 300s) passar.
- **Tela bloqueada / sem usuário logado**: a gravação deve continuar por pelo
  menos 30 min sem interrupção.
- **Uso de CPU**: com os 8 canais ativos, o conjunto deve ficar abaixo de ~15%
  numa máquina modesta (prova de que não há reencode em lugar nenhum).

## 9. Calibrando a sincronia de áudio/vídeo

Encoders IP baratos costumam entregar áudio e vídeo com um offset fixo entre si
(medido em campo no BOV02: áudio ~1.5s adiantado). Cada canal tem
`av_sync_offset_seconds` no `config.yaml`: valor positivo atrasa o áudio, negativo
adianta. Isso é aplicado só no remux via `-itsoffset` — não decodifica nem reencoda
nada, então não quebra a garantia de "mesmo codec da fonte".

Para calibrar um canal:
1. Deixe gravar alguns arquivos com um valor de teste (ex.: `1.5`).
2. Abra um `.mp4` recente no VLC e veja se áudio/vídeo bateram.
3. Se ainda estiver fora, ajuste o valor (aumente se o áudio ainda estiver
   adiantado, diminua/zere se passou a estar atrasado) e reinicie o serviço
   (`.\grtv-recorder.exe stop` / `start`) — o publisher só lê o valor no arranque.
4. Repita até bater. Cada canal pode precisar de um valor diferente, já que cada um
   vem de uma fonte/entrada distinta no encoder.

Se a defasagem estiver **piorando ao longo do clipe** (não é o caso relatado até
agora) em vez de constante, isso é deriva de clock e `av_sync_offset_seconds` não
resolve — nesse caso a única correção real exige reencodar o áudio, o que contraria
o SPEC.md (§5.3) e o critério de aceite §13.5. Não implementar sem decisão explícita.
