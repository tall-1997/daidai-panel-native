param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = "Stop"

function Write-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Fail-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "[FAIL] $Message" -ForegroundColor Red
    exit 1
}

function Assert-FileContains {
    param(
        [string]$Path,
        [string]$Pattern,
        [string]$Description
    )

    $text = Get-Content -Path $Path -Raw -Encoding UTF8
    if ($text -notmatch $Pattern) {
        Fail-Step "$Description not synced: $Path"
    }
}

function Assert-FileTextContains {
    param(
        [string]$Path,
        [string]$Text,
        [string]$Description
    )

    $content = Get-Content -Path $Path -Raw -Encoding UTF8
    if (-not $content.Contains($Text)) {
        Fail-Step "$Description not synced: $Path"
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$normalizedVersion = $Version.Trim()
if ($normalizedVersion -notmatch '^\d+\.\d+\.\d+$') {
    Fail-Step "Version must use X.Y.Z format, for example 2.2.20"
}

$tagVersion = "v$normalizedVersion"
$versionCode = $null
try {
    $parts = $normalizedVersion.Split(".")
    $versionCode = ([int]$parts[0] * 10000) + ([int]$parts[1] * 100) + ([int]$parts[2])
} catch {
    Fail-Step "Unable to compute versionCode for $normalizedVersion"
}

Set-Location $repoRoot

Write-Step "Check git worktree"
$status = git status --short
if ($status) {
    Fail-Step "Worktree is dirty. Commit or clean changes before release.`n$status"
}

Write-Step "Check version file sync"
$releaseNotePath = Join-Path $repoRoot "docs\release-notes\$tagVersion.md"
if (-not (Test-Path $releaseNotePath)) {
    Fail-Step "Missing release notes file: $releaseNotePath"
}

Assert-FileContains -Path $releaseNotePath -Pattern '<!--\s*release-title:\s*.+?\s*-->' -Description "release notes title marker"
$readmeContent = Get-Content -Path (Join-Path $repoRoot "README.md") -Raw -Encoding UTF8
if (($readmeContent -notmatch [regex]::Escape($tagVersion)) -or ($readmeContent -notmatch [regex]::Escape("./docs/release-notes/$tagVersion.md"))) {
    Fail-Step "README latest version block not synced."
}
$moduleProp = Get-Content -Path (Join-Path $repoRoot "Magisk\module.prop") -Raw -Encoding UTF8
if (($moduleProp -notmatch [regex]::Escape("version=$tagVersion")) -or ($moduleProp -notmatch [regex]::Escape("versionCode=$versionCode"))) {
    Fail-Step "Magisk module.prop version not synced."
}
$updateJson = Get-Content -Path (Join-Path $repoRoot "Magisk\update.json") -Raw -Encoding UTF8
if (($updateJson -notmatch [regex]::Escape('"version": "' + $tagVersion + '"')) `
    -or ($updateJson -notmatch [regex]::Escape('"versionCode": ' + $versionCode)) `
    -or ($updateJson -notmatch [regex]::Escape("/releases/download/$tagVersion/daidai-panel-magisk-$tagVersion.zip")) `
    -or ($updateJson -notmatch [regex]::Escape("/docs/release-notes/$tagVersion.md"))) {
    Fail-Step "Magisk update.json version block not synced."
}

Write-Step "Check Windows start.bat line endings"
$startBatPath = Join-Path $repoRoot "packaging\windows\start.bat"
if (-not (Test-Path $startBatPath)) {
    Fail-Step "Missing Windows start script: $startBatPath"
}
$startBatBytes = [System.IO.File]::ReadAllBytes($startBatPath)
for ($i = 0; $i -lt $startBatBytes.Length; $i++) {
    # Windows 用户会直接双击 start.bat，发布前必须阻止 LF 换行进入 zip 包。
    if (($startBatBytes[$i] -eq 10) -and (($i -eq 0) -or ($startBatBytes[($i - 1)] -ne 13))) {
        Fail-Step "packaging/windows/start.bat must use Windows CRLF line endings."
    }
}

Write-Step "Run backend tests"
Push-Location (Join-Path $repoRoot "server")
try {
    go test ./...
} finally {
    Pop-Location
}

Write-Step "Run frontend build"
Push-Location (Join-Path $repoRoot "web")
try {
    npm run build
} finally {
    Pop-Location
}

Write-Step "Check release workflow YAML"
$workflowPath = Join-Path $repoRoot ".github\workflows\release.yml"
if (-not (Test-Path $workflowPath)) {
    Fail-Step "Missing release workflow: $workflowPath"
}

Write-Step "Check Docker Python runtime image tags"
foreach ($dockerTag in @(
    "latest3.10",
    "latest3.11",
    "latestall",
    "debian3.10",
    "debian3.11",
    "debianall"
)) {
    Assert-FileTextContains -Path $workflowPath -Text $dockerTag -Description "Docker Python runtime tag $dockerTag"
}
Assert-FileTextContains -Path $workflowPath -Text 'platforms: ${{ matrix.platforms }}' -Description "Docker matrix-specific platform list"
Assert-FileTextContains -Path $workflowPath -Text "platforms: linux/amd64,linux/arm64" -Description "Alpine Python 3.10/3.11/all platform limit"
Assert-FileTextContains -Path $workflowPath -Text "platforms: linux/amd64,linux/arm64,linux/386,linux/arm/v7" -Description "Alpine default Python 3.12 keeps 32-bit platforms"
Assert-FileTextContains -Path (Join-Path $repoRoot "Dockerfile") -Text "PYTHON_RUNTIME_MODE" -Description "Alpine Docker Python runtime build args"
Assert-FileTextContains -Path (Join-Path $repoRoot "Dockerfile.debian") -Text "PYTHON_RUNTIME_MODE" -Description "Debian Docker Python runtime build args"

$actionlint = Get-Command actionlint -ErrorAction SilentlyContinue
if ($actionlint) {
    & $actionlint.Source $workflowPath
} else {
    Write-Host "[WARN] actionlint not found, skip local workflow lint." -ForegroundColor Yellow
}

Write-Step "Check remote tag conflict"
$remoteTagExists = git ls-remote --tags origin ("refs/tags/" + $tagVersion)
if ($remoteTagExists) {
    Fail-Step "Remote tag already exists: $tagVersion. Confirm whether you really want to re-release."
}

Write-Step "Check branch status"
$currentBranch = git branch --show-current
if ($currentBranch -ne "main") {
    Write-Host "[WARN] Current branch is $currentBranch, not main." -ForegroundColor Yellow
}

$aheadBehind = git rev-list --left-right --count origin/main...HEAD
Write-Host "origin/main...HEAD = $aheadBehind"

Write-Host ""
Write-Host "[OK] Release preflight passed: $tagVersion" -ForegroundColor Green
