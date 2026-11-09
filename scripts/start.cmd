@echo off
setlocal DisableDelayedExpansion
for %%I in ("%~dp0..") do set "PROJECT_ROOT=%%~fI"
where go >nul 2>nul
if errorlevel 1 (echo Go 1.25+ is required for source development. Use a release package to run without Go.& exit /b 1)
where npm.cmd >nul 2>nul
if errorlevel 1 (echo Node.js 22.12+ and npm are required for source development.& exit /b 1)
pushd "%PROJECT_ROOT%\web"
if not exist node_modules (
  call npm.cmd ci
  if errorlevel 1 (popd & exit /b 1)
)
call npm.cmd run build
if errorlevel 1 (popd & exit /b 1)
popd
if not exist "%PROJECT_ROOT%\.cache" mkdir "%PROJECT_ROOT%\.cache"
pushd "%PROJECT_ROOT%\server"
set "CGO_ENABLED=0"
go build -o "%PROJECT_ROOT%\.cache\lypit-studio.exe" .
if errorlevel 1 (popd & exit /b 1)
"%PROJECT_ROOT%\.cache\lypit-studio.exe" -root "%PROJECT_ROOT%" -open-browser %*
set "APP_EXIT=%errorlevel%"
popd
exit /b %APP_EXIT%
