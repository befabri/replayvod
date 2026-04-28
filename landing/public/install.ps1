$ErrorActionPreference = 'Stop'

$RepoUrl = if ($env:REPLAYVOD_REPO) { $env:REPLAYVOD_REPO } elseif ($env:REPLAYVOD_REPO_URL) { $env:REPLAYVOD_REPO_URL } else { 'https://github.com/befabri/replayvod.git' }
$Branch = if ($env:REPLAYVOD_BRANCH) { $env:REPLAYVOD_BRANCH } else { 'main' }
$Mode = if ($env:REPLAYVOD_INSTALL_MODE) { $env:REPLAYVOD_INSTALL_MODE.ToLowerInvariant() } else { 'native' }
$InstallDir = if ($env:REPLAYVOD_DIR) { $env:REPLAYVOD_DIR } elseif ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'ReplayVOD' } elseif ($HOME) { Join-Path $HOME 'ReplayVOD' } else { 'ReplayVOD' }
$EnvFile = Join-Path $InstallDir 'server/.env'
$ComposeKind = $null

function Write-Log([string]$Message) {
  [Console]::Error.WriteLine($Message)
}

function Stop-Install([string]$Message) {
  Write-Log "replayvod install: $Message"
  exit 1
}

function Have([string]$Command) {
  return $null -ne (Get-Command $Command -ErrorAction SilentlyContinue)
}

function Need([string]$Command) {
  if (-not (Have $Command)) {
    Stop-Install "missing required command: $Command"
  }
}

function New-Secret {
  $bytes = New-Object byte[] 32
  [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
  return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

function Ensure-EnvFile {
  $envDir = Split-Path -Parent $EnvFile
  New-Item -ItemType Directory -Force -Path $envDir | Out-Null

  if (Test-Path $EnvFile) {
    return
  }

  $templates = @(
    (Join-Path $InstallDir 'server/.env.example'),
    (Join-Path $InstallDir '.env.example')
  )
  foreach ($template in $templates) {
    if (Test-Path $template) {
      Copy-Item $template $EnvFile
      return
    }
  }

  @'
DATABASE_DRIVER=sqlite
TWITCH_CLIENT_ID=
TWITCH_SECRET=
HMAC_SECRET=
SESSION_SECRET=
OWNER_TWITCH_ID=
HOST=0.0.0.0
PORT=8080
CALLBACK_URL=http://localhost:8080/api/v1/auth/twitch/callback
WEBHOOK_CALLBACK_URL=http://localhost:8080/api/v1/webhook/callback
FRONTEND_URL=http://localhost:8080
PUBLIC_BASE_URL=http://localhost:8080
SQLITE_PATH=./data/replayvod.db
VIDEO_DIR=./data/videos
THUMBNAIL_DIR=./data/thumbnails
DASHBOARD_DIR=./dashboard
SCRATCH_DIR=./data/.scratch
'@ | Set-Content -Encoding ASCII $EnvFile
}

function Get-EnvValue([string]$Key) {
  if (-not (Test-Path $EnvFile)) {
    return ''
  }

  foreach ($line in [System.IO.File]::ReadLines($EnvFile)) {
    if ($line.StartsWith("$Key=")) {
      $value = $line.Substring($Key.Length + 1)
      $value = $value -replace '\s+#.*$', ''
      return $value.Trim()
    }
  }
  return ''
}

function Set-EnvValue([string]$Key, [string]$Value) {
  $lines = if (Test-Path $EnvFile) { [System.IO.File]::ReadAllLines($EnvFile) } else { @() }
  $out = New-Object System.Collections.Generic.List[string]
  $done = $false

  foreach ($line in $lines) {
    if ($line.StartsWith("$Key=")) {
      $out.Add("$Key=$Value")
      $done = $true
    } else {
      $out.Add($line)
    }
  }

  if (-not $done) {
    $out.Add("$Key=$Value")
  }

  [System.IO.File]::WriteAllLines($EnvFile, [string[]]$out, [System.Text.Encoding]::ASCII)
}

function Prompt-Env([string]$Key, [string]$Label, [switch]$Secret) {
  if (Get-EnvValue $Key) {
    return
  }
  if (-not [Environment]::UserInteractive) {
    return
  }

  if ($Secret) {
    $secure = Read-Host $Label -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
      $value = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    } finally {
      [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
    }
  } else {
    $value = Read-Host $Label
  }

  if ($value) {
    Set-EnvValue $Key $value
  }
}

function Ensure-CommonConfig {
  Ensure-EnvFile

  if (-not (Get-EnvValue 'HMAC_SECRET')) {
    Set-EnvValue 'HMAC_SECRET' (New-Secret)
  }
  if (-not (Get-EnvValue 'SESSION_SECRET')) {
    Set-EnvValue 'SESSION_SECRET' (New-Secret)
  }

  Prompt-Env 'TWITCH_CLIENT_ID' 'Twitch Client ID'
  Prompt-Env 'TWITCH_SECRET' 'Twitch Client Secret' -Secret
  Prompt-Env 'OWNER_TWITCH_ID' 'Owner Twitch numeric user ID'

  return @('TWITCH_CLIENT_ID', 'TWITCH_SECRET', 'OWNER_TWITCH_ID') | Where-Object { -not (Get-EnvValue $_) }
}

function Get-WindowsArch {
  switch ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
    'arm64' { return 'arm64' }
    default { return 'amd64' }
  }
}

function Get-NativeDownloadUrl {
  if ($env:REPLAYVOD_WINDOWS_URL) {
    return $env:REPLAYVOD_WINDOWS_URL
  }

  $arch = Get-WindowsArch
  if ($env:REPLAYVOD_VERSION) {
    return "https://github.com/befabri/replayvod/releases/download/$($env:REPLAYVOD_VERSION)/replayvod-windows-$arch.zip"
  }
  return "https://github.com/befabri/replayvod/releases/latest/download/replayvod-windows-$arch.zip"
}

function Find-ReplayVODExe {
  $direct = Join-Path $InstallDir 'replayvod.exe'
  if (Test-Path $direct) {
    return $direct
  }

  $found = Get-ChildItem -Path $InstallDir -Filter 'replayvod.exe' -Recurse -File -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($found) {
    return $found.FullName
  }
  return $null
}

function Install-Native {
  $url = Get-NativeDownloadUrl
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) "replayvod-windows-$([System.Guid]::NewGuid()).zip"

  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  try {
    Invoke-WebRequest -Uri $url -OutFile $tmp
  } catch {
    Write-Log "ReplayVOD native Windows binary is not available at $url."
    Write-Log "If you want the Docker install path instead, run:"
    Write-Log "  `$env:REPLAYVOD_INSTALL_MODE='docker'; irm https://replayvod.com/install.ps1 | iex"
    exit 1
  }

  Expand-Archive -Path $tmp -DestinationPath $InstallDir -Force
  Remove-Item $tmp -Force -ErrorAction SilentlyContinue

  $missing = Ensure-CommonConfig
  $exe = Find-ReplayVODExe
  if (-not $exe) {
    Stop-Install "release archive did not contain replayvod.exe"
  }

  if (-not (Have 'ffmpeg')) {
    Write-Log 'warning: ffmpeg was not found on PATH. Recording requires ffmpeg unless the release bundles it.'
  }

  if ($missing.Count -gt 0) {
    Write-Log ''
    Write-Log 'ReplayVOD is installed and .env was created.'
    Write-Log "Fill these values in ${EnvFile}: $($missing -join ', ')."
    Write-Log ''
    Write-Log 'Then run:'
    Write-Log "  Set-Location '$InstallDir'; .\replayvod.exe"
    exit 1
  }

  if ($env:REPLAYVOD_NO_START -eq '1') {
    Write-Log 'ReplayVOD is installed. Start it with:'
    Write-Log "  Set-Location '$InstallDir'; .\replayvod.exe"
    exit 0
  }

  Start-Process -FilePath $exe -WorkingDirectory $InstallDir
  $baseUrl = Get-EnvValue 'PUBLIC_BASE_URL'
  if (-not $baseUrl) { $baseUrl = 'http://localhost:8080' }
  Write-Host "`nReplayVOD is starting at $baseUrl"
}

function Compose-Plugin-Available {
  try {
    & docker compose version *> $null
    return $LASTEXITCODE -eq 0
  } catch {
    return $false
  }
}

function Compose-V1-Available {
  if (-not (Have 'docker-compose')) { return $false }
  try {
    & docker-compose version *> $null
    return $LASTEXITCODE -eq 0
  } catch {
    return $false
  }
}

function Compose-Command-Text([string]$Profile) {
  if ($ComposeKind -eq 'v1') {
    return "Set-Location '$InstallDir'; docker-compose --env-file server/.env --profile $Profile up -d --build"
  }
  return "Set-Location '$InstallDir'; docker compose --env-file server/.env --profile $Profile up -d --build"
}

function Origin-Is-ReplayVOD([string]$Origin) {
  return $Origin -in @($RepoUrl, 'https://github.com/befabri/replayvod', 'https://github.com/befabri/replayvod.git', 'git@github.com:befabri/replayvod.git')
}

function Install-Docker {
  Need 'git'
  Need 'docker'

  if (Compose-Plugin-Available) {
    $script:ComposeKind = 'plugin'
  } elseif (Compose-V1-Available) {
    $script:ComposeKind = 'v1'
  } else {
    Stop-Install 'Docker Compose is required (docker compose or docker-compose).'
  }

  if (Test-Path (Join-Path $InstallDir '.git')) {
    $origin = (& git -C $InstallDir remote get-url origin 2>$null) -join ''
    if (-not (Origin-Is-ReplayVOD $origin)) {
      Stop-Install "existing checkout origin is not ReplayVOD: $origin"
    }

    $currentBranch = (& git -C $InstallDir rev-parse --abbrev-ref HEAD 2>$null) -join ''
    $dirty = (& git -C $InstallDir status --porcelain) -join ''
    if ($currentBranch -ne $Branch) {
      Write-Log "replayvod install: existing checkout is on branch $currentBranch; skipping pull for requested branch $Branch."
    } elseif (-not $dirty) {
      & git -C $InstallDir pull --ff-only origin $Branch
      if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    } else {
      Write-Log 'replayvod install: existing checkout has local changes; skipping pull.'
    }
  } elseif (Test-Path $InstallDir) {
    Stop-Install "$InstallDir exists and is not a git checkout."
  } else {
    & git clone --depth=1 --branch $Branch $RepoUrl $InstallDir
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
  }

  $missing = Ensure-CommonConfig
  $profile = if ($env:REPLAYVOD_PROFILE) { $env:REPLAYVOD_PROFILE } else { Get-EnvValue 'COMPOSE_PROFILES' }
  if (-not $profile) { $profile = 'sqlite' }
  if ($profile -notin @('sqlite', 'postgres')) {
    Stop-Install "COMPOSE_PROFILES must be 'sqlite' or 'postgres' (got '$profile')."
  }
  Set-EnvValue 'COMPOSE_PROFILES' $profile

  if ($missing.Count -gt 0) {
    Write-Log ''
    Write-Log 'ReplayVOD is downloaded and server/.env was created.'
    Write-Log "Fill these values in ${EnvFile}: $($missing -join ', ')."
    Write-Log ''
    Write-Log 'Then run:'
    Write-Log "  $(Compose-Command-Text $profile)"
    exit 1
  }

  if ($env:REPLAYVOD_NO_START -eq '1') {
    Write-Log 'ReplayVOD is ready. Start it with:'
    Write-Log "  $(Compose-Command-Text $profile)"
    exit 0
  }

  try {
    & docker info *> $null
    $dockerReady = $LASTEXITCODE -eq 0
  } catch {
    $dockerReady = $false
  }

  if (-not $dockerReady) {
    Write-Log 'ReplayVOD is ready, but the Docker daemon is not reachable. Start Docker Desktop, then run:'
    Write-Log "  $(Compose-Command-Text $profile)"
    exit 0
  }

  Push-Location $InstallDir
  try {
    if ($ComposeKind -eq 'v1') {
      & docker-compose --env-file server/.env --profile $profile up -d --build
    } else {
      & docker compose --env-file server/.env --profile $profile up -d --build
    }
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
  } finally {
    Pop-Location
  }

  $baseUrl = Get-EnvValue 'PUBLIC_BASE_URL'
  if (-not $baseUrl) { $baseUrl = 'http://localhost:8080' }
  Write-Host "`nReplayVOD is starting at $baseUrl"
}

switch ($Mode) {
  'native' { Install-Native }
  'docker' { Install-Docker }
  default { Stop-Install "REPLAYVOD_INSTALL_MODE must be 'native' or 'docker' (got '$Mode')." }
}
