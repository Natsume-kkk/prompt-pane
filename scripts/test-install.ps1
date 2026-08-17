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
