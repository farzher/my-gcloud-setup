setlocal
cd /d "%~dp0"
title Google Cloud remote login

where go >nul 2>nul
if errorlevel 1 (
    echo Go was not found in PATH.
    echo Install Go 1.25 or newer, then run this again.
    echo.
    pause
    exit /b 1
)

where gcloud >nul 2>nul
if errorlevel 1 (
    echo gcloud was not found in PATH.
    echo Install the Google Cloud CLI first.
    echo.
    pause
    exit /b 1
)

echo This will show Google's login URL and render it as a QR code in this terminal.
echo After the other person approves access, paste the verification code here.
echo.

go mod tidy
if errorlevel 1 (
    echo.
    echo Dependency setup failed.
    echo.
    pause
    exit /b 1
)

go run ./cmd/remote-auth
if errorlevel 1 (
    echo.
    echo Remote login failed.
    pause
    exit /b 1
)

echo.
echo Google Cloud login complete.
pause
