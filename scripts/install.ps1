# install.ps1 — one-line installer for dpkms + ctxt on Windows
#
# Usage:
#   irm https://raw.githubusercontent.com/ideacrafterslabs/ctxt/main/scripts/install.ps1 | iex
#   # or with options:
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/ideacrafterslabs/ctxt/main/scripts/install.ps1))) -Binary ctxt -Version v0.5.0
#
# Options:
#   -Binary <name>      Binary to install: dpkms | ctxt | all (default: all)
#   -Version <tag>      Specific release tag, e.g. v0.5.0 (default: latest)
#   -InstallDir <path>  Install destination (default: $env:LOCALAPPDATA\Programs\contexthelp)
#   -NoService          Skip Windows service / Task Scheduler setup
#   -DryRun             Print actions without executing

[CmdletBinding()]
param(
    [string]$Binary     = "all",
    [string]$Version    = "",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\contexthelp",
    [switch]$NoService,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$Repo = "ideacrafterslabs/ctxt"

function info($msg)  { Write-Host "  ==> $msg" -ForegroundColor Blue }
function ok($msg)    { Write-Host "  OK  $msg" -ForegroundColor Green }
function warn($msg)  { Write-Host "  !   $msg" -ForegroundColor Yellow }
function die($msg)   { Write-Host "  X   $msg" -ForegroundColor Red; exit 1 }
function run([scriptblock]$cmd) {
    if ($DryRun) { Write-Host "  [dry-run] $cmd"; return }
    & $cmd
}

# ── architecture detection ───────────────────────────────────────────────────
$Arch = if ([System.Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    die "32-bit Windows is not supported"
}

# ── fetch latest version if not pinned ──────────────────────────────────────
if (-not $Version) {
    info "Fetching latest release tag..."
    $rel = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $rel.tag_name
    if (-not $Version) { die "Could not determine latest release version" }
}

$Ver = $Version.TrimStart("v")

info "Version : $Version"
info "Arch    : $Arch"
info "Dest    : $InstallDir"

# ── install one binary ───────────────────────────────────────────────────────
function Install-Binary([string]$Name) {
    $Archive     = "${Name}_${Ver}_windows_${Arch}.zip"
    $Url         = "https://github.com/$Repo/releases/download/$Version/$Archive"
    $ChecksumUrl = "https://github.com/$Repo/releases/download/$Version/checksums.txt"
    $Tmp         = Join-Path $env:TEMP "contexthelp-install-$Name"

    if (-not $DryRun) { New-Item -ItemType Directory -Force -Path $Tmp | Out-Null }

    info "Downloading $Name $Version..."
    if (-not $DryRun) {
        Invoke-WebRequest -Uri $Url         -OutFile (Join-Path $Tmp $Archive)
        Invoke-WebRequest -Uri $ChecksumUrl -OutFile (Join-Path $Tmp "checksums.txt")
    } else {
        Write-Host "  [dry-run] Invoke-WebRequest $Url"
    }

    # checksum validation
    if (-not $DryRun) {
        info "Validating checksum..."
        $Expected = (Get-Content (Join-Path $Tmp "checksums.txt") |
            Where-Object { $_ -match [regex]::Escape($Archive) } |
            Select-Object -First 1) -split '\s+' | Select-Object -First 1
        if ($Expected) {
            $Actual = (Get-FileHash (Join-Path $Tmp $Archive) -Algorithm SHA256).Hash.ToLower()
            if ($Actual -ne $Expected) { die "Checksum mismatch for $Archive" }
            ok "Checksum verified"
        } else {
            warn "Checksum entry not found for $Archive; skipping validation"
        }

        # extract
        Expand-Archive -Path (Join-Path $Tmp $Archive) -DestinationPath $Tmp -Force

        # install
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        $Exe = "$Name.exe"
        Copy-Item (Join-Path $Tmp $Exe) (Join-Path $InstallDir $Exe) -Force
    } else {
        Write-Host "  [dry-run] Extract and copy $Name.exe to $InstallDir"
    }

    ok "Installed $InstallDir\$Name.exe"

    if (-not $DryRun) { Remove-Item $Tmp -Recurse -Force -ErrorAction SilentlyContinue }
}

# ── install requested binary/binaries ───────────────────────────────────────
switch ($Binary) {
    "all"   { Install-Binary "ctxt"; Install-Binary "dpkms" }
    "ctxt"  { Install-Binary "ctxt" }
    "dpkms" { Install-Binary "dpkms" }
    default { die "Unknown binary: $Binary (choose: ctxt | dpkms | all)" }
}

# ── PATH hint ────────────────────────────────────────────────────────────────
$UserPath = [System.Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notlike "*$InstallDir*") {
    warn "$InstallDir is not in your PATH."
    if (-not $DryRun) {
        $NewPath = "$InstallDir;$UserPath"
        [System.Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
        ok "Added $InstallDir to user PATH (restart your terminal to apply)"
    } else {
        Write-Host "  [dry-run] Would add $InstallDir to user PATH"
    }
}

# ── service setup (dpkms serve) ─────────────────────────────────────────────
if (-not $NoService -and ($Binary -eq "all" -or $Binary -eq "dpkms")) {
    info "Setting up Windows Task Scheduler service for dpkms..."
    $TaskName = "ContextHelp.dpkms"
    $DpkmsExe = Join-Path $InstallDir "dpkms.exe"

    if (-not $DryRun) {
        $Action  = New-ScheduledTaskAction -Execute $DpkmsExe -Argument "serve"
        $Trigger = New-ScheduledTaskTrigger -AtLogOn
        $Settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
        $Principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest

        Register-ScheduledTask -TaskName $TaskName `
            -Action $Action -Trigger $Trigger `
            -Settings $Settings -Principal $Principal `
            -Force | Out-Null

        Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        ok "Task Scheduler service registered: $TaskName"
    } else {
        Write-Host "  [dry-run] Would register Task Scheduler task: $TaskName"
    }
}

# ── first-run hint ───────────────────────────────────────────────────────────
if ($Binary -eq "all" -or $Binary -eq "dpkms") {
    info "Run 'dpkms serve' to start the server (migrations run automatically on first launch)."
}

ok "Installation complete."
