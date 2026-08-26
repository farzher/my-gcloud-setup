@echo off
setlocal
cd /d "%~dp0"
title my-gcloud-setup

where git >nul 2>nul
if not errorlevel 1 (
    git pull --ff-only --quiet >nul 2>nul
)

where go >nul 2>nul
if errorlevel 1 (
    echo Go was not found in PATH.
    echo Install Go 1.25 or newer, then run this again.
    echo.
    pause
    exit /b 1
)

if not exist go.sum (
    go mod tidy
    if errorlevel 1 (
        echo.
        echo Dependency setup failed.
        echo.
        pause
        exit /b 1
    )
)

go run .
if errorlevel 1 (
    echo.
    echo Run failed. Refreshing dependencies and retrying...
    go mod tidy
    if errorlevel 1 (
        echo.
        echo Dependency setup failed.
        echo.
        pause
        exit /b 1
    )
    go run .
    if errorlevel 1 (
        echo.
        echo Run failed.
        echo.
        pause
        exit /b 1
    )
)
