setlocal
cd /d "%~dp0"
title my-gcloud-setup

where git >nul 2>nul
if not errorlevel 1 (
    git status --short --branch
    git pull --ff-only
    if errorlevel 1 (
        echo.
        echo Git update failed. Fix or reset the local checkout before running cloud.
        echo Refusing to run stale source.
        echo.
        pause
        exit /b 1
    )
)

where go >nul 2>nul
if errorlevel 1 (
    echo Go was not found in PATH.
    echo Install Go 1.25 or newer, then run this again.
    echo.
    pause
    exit /b 1
)

where gh >nul 2>nul
if errorlevel 1 (
    where winget >nul 2>nul
    if not errorlevel 1 (
        echo Installing GitHub CLI...
        winget install --id GitHub.cli -e --silent --accept-package-agreements --accept-source-agreements
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
