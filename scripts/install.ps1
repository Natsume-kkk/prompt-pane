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

    if ($PSVersionTable.PSVersion.Major -lt 5) {
        throw "PowerShell 5.1 or PowerShell 7 is required."
    }
}

function Resolve-InstallRoot {
    if ($env:PROMPT_PANE_HOME) {
        if (-not [IO.Path]::IsPathRooted($env:PROMPT_PANE_HOME)) {
            throw "PROMPT_PANE_HOME must be an absolute path."
        }
        return [IO.Path]::GetFullPath($env:PROMPT_PANE_HOME)
    }

    $applicationData = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
    if (-not $applicationData) {
        throw "Cannot locate the current user's application data directory."
    }
    return Join-Path $applicationData "PromptPane"
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

function Install-Binary {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$ExpectedHash
    )

    $actualHash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    if ($actualHash -ne $ExpectedHash) {
        throw "The downloaded Prompt Pane executable does not match the Release SHA-256. The file was not installed; retry the download and report the Release if the mismatch persists."
    }

    $destinationDirectory = Split-Path -Parent $Destination
    try {
        New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null
    } catch {
        throw "Cannot prepare the Prompt Pane install directory $destinationDirectory. Check write permissions and available disk space. Reason: $($_.Exception.Message)"
    }

    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
        $installedHash = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
        if ($installedHash -eq $actualHash) {
            Write-Host "[3/4] Prompt Pane is already up to date."
            return $null
        }
    }

    $staged = Join-Path $destinationDirectory (".prompt-pane-{0}.tmp" -f [Guid]::NewGuid().ToString("N"))
    $backup = Join-Path $destinationDirectory (".prompt-pane-{0}.bak" -f [Guid]::NewGuid().ToString("N"))
    try {
        Copy-Item -LiteralPath $Source -Destination $staged
    } catch {
        throw "Cannot stage Prompt Pane in $destinationDirectory. Check write permissions and available disk space. Reason: $($_.Exception.Message)"
    }

    try {
        try {
            if (Test-Path -LiteralPath $Destination -PathType Leaf) {
                [IO.File]::Replace($staged, $Destination, $backup, $true)
                return $backup
            }

            Move-Item -LiteralPath $staged -Destination $Destination
            return ""
        } catch {
            throw "Cannot activate Prompt Pane at $Destination. Close running Prompt Pane processes and check write permissions, then retry. Reason: $($_.Exception.Message)"
        }
    } finally {
        if (Test-Path -LiteralPath $staged) {
            Remove-Item -LiteralPath $staged -Force
        }
    }
}

function Restore-PreviousBinary {
    param(
        [Parameter(Mandatory = $true)][string]$Destination,
        [AllowEmptyString()][string]$Backup
    )

    if ($Backup -and (Test-Path -LiteralPath $Backup -PathType Leaf)) {
        $discard = Join-Path (Split-Path -Parent $Destination) (".prompt-pane-{0}.discard" -f [Guid]::NewGuid().ToString("N"))
        try {
            [IO.File]::Replace($Backup, $Destination, $discard, $true)
        } finally {
            if (Test-Path -LiteralPath $discard) {
                Remove-Item -LiteralPath $discard -Force
            }
        }
        return
    }

    if ($Backup -eq "" -and (Test-Path -LiteralPath $Destination -PathType Leaf)) {
        Remove-Item -LiteralPath $Destination -Force
    }
}

Assert-SupportedEnvironment

$releaseBaseUrl = Resolve-ReleaseBaseUrl
$installRoot = Resolve-InstallRoot
$destination = Join-Path (Join-Path $installRoot "bin") $assetName
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("prompt-pane-install-{0}" -f [Guid]::NewGuid().ToString("N"))
$downloadedBinary = Join-Path $temporaryDirectory $assetName
$downloadedChecksum = Join-Path $temporaryDirectory $checksumName
$previousSecurityProtocol = [Net.ServicePointManager]::SecurityProtocol
$backup = $null

try {
    try {
        New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
    } catch {
        throw "Cannot create the Prompt Pane temporary download directory. Check temporary-directory permissions and available disk space. Reason: $($_.Exception.Message)"
    }
    [Net.ServicePointManager]::SecurityProtocol = $previousSecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    Write-Host "[1/4] Downloading Prompt Pane $Version..."
    Invoke-Download -Uri "$releaseBaseUrl/$assetName" -Destination $downloadedBinary -Artifact "Prompt Pane executable"
    Invoke-Download -Uri "$releaseBaseUrl/$checksumName" -Destination $downloadedChecksum -Artifact "Prompt Pane checksum"

    Write-Host "[2/4] Verifying SHA-256..."
    $expectedHash = Read-ExpectedHash -Path $downloadedChecksum
    $backup = Install-Binary -Source $downloadedBinary -Destination $destination -ExpectedHash $expectedHash
    if ($null -ne $backup) {
        Write-Host "[3/4] Installed Prompt Pane for the current user."
    }

    Write-Host "[4/4] Configuring Codex integration..."
    & $destination setup codex
    if ($LASTEXITCODE -ne 0) {
        throw "Prompt Pane setup failed with exit code $LASTEXITCODE. Review the preceding prompt-pane error for the failed component and corrective action."
    }

    if ($backup -and (Test-Path -LiteralPath $backup)) {
        Remove-Item -LiteralPath $backup -Force
    }

    Write-Host ""
    Write-Host "Installation complete. Run codex.pp to start."
} catch {
    $installError = $_
    if ($null -ne $backup) {
        try {
            Restore-PreviousBinary -Destination $destination -Backup $backup
        } catch {
            Write-Warning "Could not restore the previous Prompt Pane executable: $($_.Exception.Message)"
        }
    }
    throw $installError
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
