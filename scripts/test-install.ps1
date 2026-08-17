[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

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

    $source = Join-Path $temporaryRoot "source.exe"
    $destination = Join-Path $temporaryRoot "用户 path\prompt-pane.exe"

    [IO.File]::WriteAllBytes($source, [byte[]](1, 2, 3, 4))
    $originalHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
    $firstInstall = Install-Binary -Source $source -Destination $destination -ExpectedHash $originalHash
    if ($null -eq $firstInstall -or $firstInstall -ne "") {
        throw "First-install rollback marker is invalid."
    }

    [IO.File]::WriteAllBytes($source, [byte[]](5, 6, 7))
    $replacementHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
    $backup = Install-Binary -Source $source -Destination $destination -ExpectedHash $replacementHash
    if (-not $backup -or -not (Test-Path -LiteralPath $backup -PathType Leaf)) {
        throw "Replacement did not create a rollback copy."
    }
    Restore-PreviousBinary -Destination $destination -Backup $backup
    if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $originalHash) {
        throw "Rollback did not restore the previous executable."
    }

    $checksumRejected = $false
    try {
        Install-Binary -Source $source -Destination $destination -ExpectedHash ("0" * 64) | Out-Null
    } catch {
        $checksumRejected = $true
        foreach ($expected in @("does not match", "was not installed", "retry")) {
            if (-not $_.Exception.Message.Contains($expected)) {
                throw "Checksum mismatch guidance is missing: $expected"
            }
        }
    }
    if (-not $checksumRejected -or (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash -ne $originalHash) {
        throw "Checksum rejection changed the installed executable."
    }

    $env:PROMPT_PANE_HOME = Join-Path $temporaryRoot "中文 root"
    if ((Resolve-InstallRoot) -ne [IO.Path]::GetFullPath($env:PROMPT_PANE_HOME)) {
        throw "The custom install root was not preserved."
    }

    Write-Output "install.ps1 tests passed"
} finally {
    $env:PROMPT_PANE_HOME = $null
    if (Test-Path -LiteralPath $temporaryRoot) {
        Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
    }
}
