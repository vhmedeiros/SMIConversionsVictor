@echo off
REM Remove o servico GRTVRecorder (SPEC.md §10.2). Rode como Administrador.

set BIN=C:\Sistema\bin\grtv-recorder.exe

echo Parando servico (se estiver rodando)...
sc stop GRTVRecorder

echo Removendo servico...
"%BIN%" uninstall
if errorlevel 1 (
    echo Falha ao remover via grtv-recorder.exe uninstall. Tentando via sc delete...
    sc delete GRTVRecorder
)

echo.
echo Servico GRTVRecorder removido.
echo Os diretorios work\ e arquivos\stream\ NAO foram apagados.
