[CmdletBinding()]
param(
    [ValidatePattern("^(latest|v?[0-9]+\.[0-9]+\.[0-9]+)$")]
    [string]$Version = "latest"
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"

$repository = "Natsume-kkk/prompt-pane"
$assetName = "prompt-pane.exe"
$checksumName = "prompt-pane.exe.sha256"

function Assert-SupportedEnvironment {
    if ($env:OS -ne "Windows_NT") {
        throw "Prompt Pane supports Windows x64 only."
    }

    $architecture = $env:PROCESSOR_ARCHITECTURE
    $nativeArchitecture = $env:PROCESSOR_ARCHITEW6432
    if ($architecture -ne "AMD64" -and $nativeArchitecture -ne "AMD64") {
        throw "Prompt Pane supports Windows x64 only. Detected architecture: $architecture."
    }

    if (-not (Test-SupportedPowerShellVersion -Version $PSVersionTable.PSVersion)) {
        throw "PowerShell 5.1 or PowerShell 7 is required. Detected PowerShell $($PSVersionTable.PSVersion)."
    }
}

function Test-SupportedPowerShellVersion {
    param([Parameter(Mandatory = $true)][version]$Version)

    return ($Version.Major -eq 5 -and $Version.Minor -ge 1) -or $Version.Major -ge 7
}

function Resolve-ReleaseBaseUrl {
    if ($Version -eq "latest") {
        return "https://github.com/$repository/releases/latest/download"
    }

    $tag = $Version
    if (-not $tag.StartsWith("v", [StringComparison]::OrdinalIgnoreCase)) {
        $tag = "v$tag"
    }
    return "https://github.com/$repository/releases/download/$tag"
}

function Invoke-Download {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$Artifact
    )

    try {
        Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination
    } catch {
        throw (Format-DownloadFailure -Artifact $Artifact -Uri $Uri -Reason $_.Exception.Message)
    }
    if (-not (Test-Path -LiteralPath $Destination -PathType Leaf)) {
        throw "Downloaded $Artifact was not saved. Check temporary-directory permissions and available disk space, then retry. Source: $Uri"
    }
}

function Format-DownloadFailure {
    param(
        [Parameter(Mandatory = $true)][string]$Artifact,
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$Reason
    )

    $safeReason = $Reason -replace '(?i)(https?://)[^/\s@]+@', '$1***@'
    return "Failed to download $Artifact from GitHub. Check that the requested Release and asset exist and that this PowerShell session can access GitHub through its proxy and TLS settings. Source: $Uri. Reason: $safeReason"
}

function Read-ExpectedHash {
    param([Parameter(Mandatory = $true)][string]$Path)

    $contents = Get-Content -LiteralPath $Path -Raw
    $match = [regex]::Match($contents, "(?i)\b[0-9a-f]{64}\b")
    if (-not $match.Success) {
        throw "The Prompt Pane checksum asset does not contain a 64-character SHA-256 value. Download the executable and checksum from the same GitHub Release, then retry."
    }
    return $match.Value.ToUpperInvariant()
}

function Get-SHA256Hash {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::OpenRead($Path)
    try {
        $sha256 = [Security.Cryptography.SHA256]::Create()
        try {
            return ([BitConverter]::ToString($sha256.ComputeHash($stream))).Replace("-", "")
        } finally {
            $sha256.Dispose()
        }
    } finally {
        $stream.Dispose()
    }
}

function Assert-DownloadedBinary {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$ExpectedHash
    )

    $actualHash = Get-SHA256Hash -Path $Path
    if ($actualHash -ne $ExpectedHash) {
        throw "The downloaded Prompt Pane executable does not match the Release SHA-256. The file was not installed; retry the download and report the Release if the mismatch persists."
    }
}

Assert-SupportedEnvironment

$releaseBaseUrl = Resolve-ReleaseBaseUrl
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("prompt-pane-install-{0}" -f [Guid]::NewGuid().ToString("N"))
$downloadedBinary = Join-Path $temporaryDirectory $assetName
$downloadedChecksum = Join-Path $temporaryDirectory $checksumName
$previousSecurityProtocol = [Net.ServicePointManager]::SecurityProtocol

try {
    try {
        New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
    } catch {
        throw "Cannot create the Prompt Pane temporary download directory. Check temporary-directory permissions and available disk space. Reason: $($_.Exception.Message)"
    }
    [Net.ServicePointManager]::SecurityProtocol = $previousSecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    Write-Host "[1/3] Downloading Prompt Pane $Version..."
    Invoke-Download -Uri "$releaseBaseUrl/$assetName" -Destination $downloadedBinary -Artifact "Prompt Pane executable"
    Invoke-Download -Uri "$releaseBaseUrl/$checksumName" -Destination $downloadedChecksum -Artifact "Prompt Pane checksum"

    Write-Host "[2/3] Verifying SHA-256..."
    $expectedHash = Read-ExpectedHash -Path $downloadedChecksum
    Assert-DownloadedBinary -Path $downloadedBinary -ExpectedHash $expectedHash

    Write-Host "[3/3] Installing Prompt Pane and configuring Codex integration..."
    & $downloadedBinary setup codex
    if ($LASTEXITCODE -ne 0) {
        throw "Prompt Pane setup failed with exit code $LASTEXITCODE. No previously active version was replaced. Review the preceding prompt-pane error for the failed component and corrective action."
    }

    Write-Host ""
    Write-Host "Installer finished. Follow the Prompt Pane status above."
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $previousSecurityProtocol
    if (Test-Path -LiteralPath $temporaryDirectory) {
        try {
            Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
        } catch {
            Write-Warning "Could not remove the Prompt Pane temporary download directory: $($_.Exception.Message)"
        }
    }
}
