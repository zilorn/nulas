# Windows bootstrap; existing Python, Git, Node and Go are reused when suitable.
param(
    [string]$HomeDir = "$env:LOCALAPPDATA\Nulas",
    [string]$Repository = 'https://github.com/zilorn/nulas.git',
    [string]$Branch = 'main'
)
$ErrorActionPreference = 'Stop'
Write-Host 'Nulas installer: preparing Python, build tools and the managed Mihomo core.'
Write-Host 'Downloads and builds can take several minutes; progress is printed as it runs.'
function Find-Python {
    foreach ($name in @('python', 'python3')) {
        $candidate = Get-Command $name -ErrorAction SilentlyContinue
        if ($candidate) {
            & $candidate.Source -c "import sys; sys.exit(0 if sys.version_info >= (3,10) else 1)" 2>$null
            if ($LASTEXITCODE -eq 0) { return $candidate.Source }
        }
    }
    $py = Get-Command py -ErrorAction SilentlyContinue
    if ($py) {
        $found = & $py.Source -3 -c "import sys; assert sys.version_info >= (3,10); print(sys.executable)" 2>$null
        if ($LASTEXITCODE -eq 0) { return $found }
    }
    return $null
}
$python = Find-Python
if (-not $python) {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        throw 'Install Python 3.10+ or Microsoft App Installer (winget), then rerun.'
    }
    Write-Host 'Python 3 was not found; installing it first...'
    & winget install --exact --id Python.Python.3.12 --source winget --accept-package-agreements --accept-source-agreements
    if ($LASTEXITCODE -ne 0) { throw 'Python installation failed.' }
    $env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
    $python = Find-Python
    if (-not $python) { throw 'Python is unavailable. Reopen the terminal and rerun.' }
}
$localSetup = Join-Path $PSScriptRoot 'setup.py'
$tempDir = $null
try {
    if (-not (Test-Path $localSetup)) {
        Write-Host 'Downloading the Nulas setup script...'
        $tempDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
        New-Item -ItemType Directory $tempDir | Out-Null
        $localSetup = Join-Path $tempDir 'setup.py'
        Invoke-WebRequest -UseBasicParsing 'https://raw.githubusercontent.com/zilorn/nulas/main/scripts/setup.py' -OutFile $localSetup
    }
    & $python $localSetup install --home $HomeDir --repository $Repository --branch $Branch
    if ($LASTEXITCODE -ne 0) { throw 'Nulas installation failed.' }
} finally {
    if ($tempDir) { Remove-Item -Recurse -Force $tempDir }
}
