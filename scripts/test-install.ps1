[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$unicodeUser = -join ([char[]](0x7528, 0x6237))

$installer = Join-Path $PSScriptRoot "install.ps1"
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($installer, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) {
    throw $parseErrors[0].Message
}

$ast.FindAll({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst]
}, $true) | ForEach-Object {
    Invoke-Expression ("function " + $_.Name + " " + $_.Body.Extent.Text)
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ("prompt-pane-installer-test-{0}" -f [Guid]::NewGuid().ToString("N"))
$temporaryRoot = [IO.Path]::GetFullPath($temporaryRoot)
$systemTemporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
if (-not $temporaryRoot.StartsWith($systemTemporaryRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use a temporary path outside the system temporary directory."
}

try {
    New-Item -ItemType Directory -Path $temporaryRoot | Out-Null

    foreach ($version in @("5.1", "7.0", "7.6")) {
        if (-not (Test-SupportedPowerShellVersion -Version ([version]$version))) {
            throw "Supported PowerShell version was rejected: $version"
        }
    }
    foreach ($version in @("5.0", "6.2")) {
        if (Test-SupportedPowerShellVersion -Version ([version]$version)) {
            throw "Unsupported PowerShell version was accepted: $version"
        }
    }

    $downloadFailure = Format-DownloadFailure `
        -Artifact "Prompt Pane checksum" `
        -Uri "https://github.com/example/releases/download/v1.1.0/prompt-pane.exe.sha256" `
        -Reason "synthetic network failure"
    foreach ($expected in @("Prompt Pane checksum", "requested Release and asset", "proxy and TLS", "synthetic network failure")) {
        if (-not $downloadFailure.Contains($expected)) {
            throw "Download failure guidance is missing: $expected"
        }
    }

    $redactedFailure = Format-DownloadFailure `
        -Artifact "Prompt Pane executable" `
        -Uri "https://github.com/example/prompt-pane.exe" `
        -Reason "proxy http://synthetic-user:synthetic-password@proxy.example failed"
    if ($redactedFailure.Contains("synthetic-user") -or $redactedFailure.Contains("synthetic-password") -or -not $redactedFailure.Contains("http://***@proxy.example")) {
        throw "Download failure exposed synthetic proxy credentials."
    }

    $wrappedFailure = $false
    try {
        Invoke-Download `
            -Uri "invalid://prompt-pane-test" `
            -Destination (Join-Path $temporaryRoot "unreachable.exe") `
            -Artifact "Prompt Pane executable"
    } catch {
        $wrappedFailure = $true
        foreach ($expected in @("Prompt Pane executable", "GitHub", "proxy and TLS")) {
            if (-not $_.Exception.Message.Contains($expected)) {
                throw "Wrapped download failure guidance is missing: $expected"
            }
        }
    }
    if (-not $wrappedFailure) {
        throw "Synthetic download failure was accepted."
    }

    $invalidChecksum = Join-Path $temporaryRoot "invalid.sha256"
    Set-Content -LiteralPath $invalidChecksum -Value "not-a-checksum" -Encoding Ascii
    try {
        Read-ExpectedHash -Path $invalidChecksum | Out-Null
        throw "Invalid checksum content was accepted."
    } catch {
        foreach ($expected in @("64-character SHA-256", "same GitHub Release")) {
            if (-not $_.Exception.Message.Contains($expected)) {
                throw "Checksum failure guidance is missing: $expected"
            }
        }
    }

    $source = Join-Path $temporaryRoot "$unicodeUser path\prompt-pane.exe"

    New-Item -ItemType Directory -Path (Split-Path -Parent $source) | Out-Null
    [IO.File]::WriteAllBytes($source, [byte[]](1, 2, 3, 4))
    $originalHash = Get-SHA256Hash -Path $source
    Assert-DownloadedBinary -Path $source -ExpectedHash $originalHash

    $checksumRejected = $false
    try {
        Assert-DownloadedBinary -Path $source -ExpectedHash ("0" * 64)
    } catch {
        $checksumRejected = $true
        foreach ($expected in @("does not match", "was not installed", "retry")) {
            if (-not $_.Exception.Message.Contains($expected)) {
                throw "Checksum mismatch guidance is missing: $expected"
            }
        }
    }
    if (-not $checksumRejected -or (Get-SHA256Hash -Path $source) -ne $originalHash) {
        throw "Checksum rejection changed the downloaded executable."
    }

    Write-Output "install.ps1 tests passed"
} finally {
    if (Test-Path -LiteralPath $temporaryRoot) {
        Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
    }
}
