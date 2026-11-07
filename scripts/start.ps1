$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Source startup requires Go 1.25+. Use a release package to run without Go.' }
if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw 'Source startup requires Node.js 22.12+ and npm.' }
$oldCGO = $env:CGO_ENABLED
try {
    Push-Location (Join-Path $projectRoot 'web')
    try {
        if (-not (Test-Path 'node_modules')) { & npm.cmd ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
    } finally { Pop-Location }
    $binary = Join-Path $projectRoot '.cache/lypit-studio.exe'
    New-Item -ItemType Directory -Force (Split-Path -Parent $binary) | Out-Null
    Push-Location (Join-Path $projectRoot 'server')
    try {
        $env:CGO_ENABLED = '0'
        & go build -o $binary .
        if ($LASTEXITCODE -ne 0) { throw 'Backend build failed' }
        & $binary -root $projectRoot @args
        $appExit = $LASTEXITCODE
    } finally { Pop-Location }
} finally { $env:CGO_ENABLED = $oldCGO }
exit $appExit
