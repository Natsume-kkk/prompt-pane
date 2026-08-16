@echo off
setlocal

powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0build.ps1"
exit /b %ERRORLEVEL%
