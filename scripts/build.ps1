[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Get-SHA256Hash {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::OpenRead($Path)
    try {
        $sha256 = [Security.Cryptography.SHA256]::Create()
        try {
            return ([BitConverter]::ToString($sha256.ComputeHash($stream))).Replace("-", "").ToLowerInvariant()
        } finally {
            $sha256.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$localGo = Join-Path $repoRoot ".tools\go\bin\go.exe"
$go = if (Test-Path -LiteralPath $localGo -PathType Leaf) {
    $localGo
} else {
    (Get-Command go -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
}

$distDir = Join-Path $repoRoot "dist"
$output = Join-Path $distDir "prompt-pane.exe"
$previousGoCache = $env:GOCACHE
$previousGoPath = $env:GOPATH

try {
    New-Item -ItemType Directory -Force $distDir | Out-Null
    $env:GOCACHE = Join-Path $repoRoot ".tools\gocache"
    $env:GOPATH = Join-Path $repoRoot ".tools\gopath"

    Push-Location $repoRoot
    try {
        & $go build -trimpath -o $output .
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed with exit code $LASTEXITCODE"
        }
        $hash = Get-SHA256Hash -Path $output
        Set-Content -LiteralPath "$output.sha256" -Value "$hash  prompt-pane.exe" -Encoding Ascii
    } finally {
        Pop-Location
    }
} finally {
    $env:GOCACHE = $previousGoCache
    $env:GOPATH = $previousGoPath
}
