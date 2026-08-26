setlocal
cd /d "%~dp0"
title Google Cloud remote login

where gcloud >nul 2>nul
if errorlevel 1 (
    echo gcloud was not found in PATH.
    echo Install the Google Cloud CLI first.
    echo.
    pause
    exit /b 1
)

echo Google will print an authorization URL below.
echo Open it on the other person's device, or paste it into any QR-code generator.
echo After they approve access, paste the verification code back here.
echo.

gcloud auth login --no-launch-browser
if errorlevel 1 (
    echo.
    echo Login failed.
    pause
    exit /b 1
)

echo.
echo Google Cloud login complete.
pause
