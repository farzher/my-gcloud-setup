@echo off
setlocal
cd /d "%~dp0"
title my-gcloud-setup

where go >nul 2>nul
if errorlevel 1 (
    echo Go was not found in PATH.
    echo Install Go 1.25 or newer, then run this again.
    echo.
    pause
    exit /b 1
)

go build -trimpath -ldflags="-s -w" -o cloud.exe .
if errorlevel 1 (
    echo.
    echo Build failed.
    echo.
    pause
    exit /b 1
)

cloud.exe
