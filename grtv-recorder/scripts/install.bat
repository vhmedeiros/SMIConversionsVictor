@echo off
REM Registra o servico GRTVRecorder e configura recuperacao/dependencias (SPEC.md §10.2).
REM Rode como Administrador, a partir da pasta onde grtv-recorder.exe esta instalado.

set BIN=C:\Sistema\bin\grtv-recorder.exe

echo Registrando servico...
"%BIN%" install
if errorlevel 1 (
    echo Falha ao registrar o servico via grtv-recorder.exe install.
    echo Tentando via sc create...
    sc create GRTVRecorder ^
        binPath= "%BIN%" ^
        start= delayed-auto ^
        DisplayName= "GRTV Recorder"
)

sc description GRTVRecorder "Gravacao continua de 8 canais de TV em blocos de 4 minutos"

REM Reinicia sozinho em caso de falha: 10s, 30s, 60s; contador zera a cada 24h.
sc failure GRTVRecorder reset= 86400 actions= restart/10000/restart/30000/restart/60000

REM Aguarda a rede estar pronta antes de subir (evita falha em boot).
sc config GRTVRecorder depend= Tcpip/Dnscache

REM start= delayed-auto: da margem para a interface de rede e o volume G: ficarem prontos.
sc config GRTVRecorder start= delayed-auto

echo.
echo Servico GRTVRecorder instalado.
echo Antes de iniciar, confira:
echo   - config.yaml ao lado do executavel, com os caminhos corretos;
echo   - se G: e volume local, LocalSystem funciona; se for rede mapeada, configure
echo     "sc config GRTVRecorder obj= .\usuario password= senha" com caminho UNC (SPEC.md §10.3);
echo   - powercfg /change standby-timeout-ac 0  e  powercfg /hibernate off (SPEC.md §10.4);
echo   - antivirus excluindo work\ e arquivos\stream\ da verificacao em tempo real.
echo.
echo Para iniciar: sc start GRTVRecorder   (ou grtv-recorder.exe start)
