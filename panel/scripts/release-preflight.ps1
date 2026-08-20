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
if ($LASTEXITCODE -ne 0) {
    Fail-Step "Unable to inspect git worktree."
}
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
# handler.Version 不只用于展示：CheckUpdate / 静默更新 / FinalizePendingAutoUpdateOnStartup 都以它为比较基准。
# 它一旦滞后，已是最新版的实例会反复提示更新，升级成功后还会误报“静默更新失败”，所以必须在打 tag 前拦住。
Assert-FileTextContains `
    -Path (Join-Path $repoRoot "server\handler\version.go") `
    -Text ('Version = "' + $normalizedVersion + '"') `
    -Description "backend Version constant"
Assert-FileTextContains `
    -Path (Join-Path $repoRoot "web\package.json") `
    -Text ('"version": "' + $normalizedVersion + '"') `
    -Description "frontend package.json version"
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
# Debian flavor 从 v3.0.3 起有独立的 update json。漏改这份，Debian 用户在管理器里
# 点更新会拿到旧版本号或错误的 zipUrl，静默退回 Alpine 版。
$updateJsonDebian = Get-Content -Path (Join-Path $repoRoot "Magisk\update-debian.json") -Raw -Encoding UTF8
if (($updateJsonDebian -notmatch [regex]::Escape('"version": "' + $tagVersion + '"')) `
    -or ($updateJsonDebian -notmatch [regex]::Escape('"versionCode": ' + $versionCode)) `
    -or ($updateJsonDebian -notmatch [regex]::Escape("/releases/download/$tagVersion/daidai-panel-magisk-debian-$tagVersion.zip")) `
    -or ($updateJsonDebian -notmatch [regex]::Escape("/docs/release-notes/$tagVersion.md"))) {
    Fail-Step "Magisk update-debian.json version block not synced."
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
    if ($LASTEXITCODE -ne 0) {
        Fail-Step "Backend tests failed."
    }
} finally {
    Pop-Location
}

Write-Step "Run frontend build"
Push-Location (Join-Path $repoRoot "web")
try {
    npm run build
    if ($LASTEXITCODE -ne 0) {
        Fail-Step "Frontend build failed."
    }
} finally {
    Pop-Location
}

Write-Step "Check release workflow YAML"
$workflowPath = Join-Path $repoRoot ".github\workflows\release.yml"
if (-not (Test-Path $workflowPath)) {
    Fail-Step "Missing release workflow: $workflowPath"
}

Write-Step "Check Docker image release matrix"
$workflowText = Get-Content -Path $workflowPath -Raw -Encoding UTF8
$alpineJobMatch = [regex]::Match($workflowText, '(?ms)^  docker-alpine:\s*\r?\n(?<body>.*?)(?=^  docker-debian:\s*\r?\n)')
$debianJobMatch = [regex]::Match($workflowText, '(?ms)^  docker-debian:\s*\r?\n(?<body>.*)\z')
if (-not $alpineJobMatch.Success -or -not $debianJobMatch.Success) {
    Fail-Step "Docker Alpine or Debian release job is missing."
}

# 每一项都写出预期值，防止标签存在但 Python 版本、工具模式或平台配错 (matrix)
$expectedDockerMatrix = @(
    [pscustomobject]@{ Job = "alpine"; Tag = "latest";       LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "";                 LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64,linux/386,linux/arm/v7" };
    [pscustomobject]@{ Job = "alpine"; Tag = "latest-full";  LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "-full";            LegacySuffix = "";             FullTools = "true";  Platforms = "linux/amd64,linux/arm64,linux/386,linux/arm/v7" };
    [pscustomobject]@{ Job = "alpine"; Tag = "latest-3.10";  LegacyTag = "latest3.10"; Mode = "single"; Python = "3.10"; Suffix = "-3.10";            LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "alpine"; Tag = "latest-3.11";  LegacyTag = "latest3.11"; Mode = "single"; Python = "3.11"; Suffix = "-3.11";            LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "alpine"; Tag = "latest-all";   LegacyTag = "latestall";  Mode = "all";    Python = "3.12"; Suffix = "-all";             LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian";       LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "-debian";          LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64,linux/arm/v7" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-full";  LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "-debian-full";     LegacySuffix = "";             FullTools = "true";  Platforms = "linux/amd64,linux/arm64,linux/arm/v7" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-3.10";  LegacyTag = "debian3.10"; Mode = "single"; Python = "3.10"; Suffix = "-debian-3.10";     LegacySuffix = "-debian3.10";  FullTools = "false"; Platforms = "linux/amd64,linux/arm64,linux/arm/v7" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-3.11";  LegacyTag = "debian3.11"; Mode = "single"; Python = "3.11"; Suffix = "-debian-3.11";     LegacySuffix = "-debian3.11";  FullTools = "false"; Platforms = "linux/amd64,linux/arm64,linux/arm/v7" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-all";   LegacyTag = "debianall";  Mode = "all";    Python = "3.12"; Suffix = "-debian-all";      LegacySuffix = "-debianall";   FullTools = "false"; Platforms = "linux/amd64,linux/arm64,linux/arm/v7" }
)

$alpineJobText = $alpineJobMatch.Groups["body"].Value
$debianJobText = $debianJobMatch.Groups["body"].Value
if ([regex]::Matches($alpineJobText, '(?m)^          - tag_channel:').Count -ne 5) {
    Fail-Step "Docker Alpine matrix must contain exactly 5 official tags."
}
if ([regex]::Matches($debianJobText, '(?m)^          - tag_channel:').Count -ne 5) {
    Fail-Step "Docker Debian matrix must contain exactly 5 official tags."
}

foreach ($expected in $expectedDockerMatrix) {
    $jobText = if ($expected.Job -eq "alpine") { $alpineJobText } else { $debianJobText }
    $entryPattern = '(?ms)^          - tag_channel:\s*' + [regex]::Escape($expected.Tag) + '\s*\r?\n(?<body>.*?)(?=^          - tag_channel:|^    steps:)'
    $entryMatch = [regex]::Match($jobText, $entryPattern)
    if (-not $entryMatch.Success) {
        Fail-Step "Missing official Docker tag in $($expected.Job) matrix: $($expected.Tag)"
    }

    $expectedFields = [ordered]@{
        legacy_tag_channel = $expected.LegacyTag
        python_mode = $expected.Mode
        python_version = $expected.Python
        version_suffix = $expected.Suffix
        legacy_version_suffix = $expected.LegacySuffix
        full_tools = $expected.FullTools
        platforms = $expected.Platforms
    }
    foreach ($field in $expectedFields.Keys) {
        $fieldPattern = '(?m)^            ' + [regex]::Escape($field) + ':\s*(?<value>.*?)\s*$'
        $fieldMatch = [regex]::Match($entryMatch.Groups["body"].Value, $fieldPattern)
        if (-not $fieldMatch.Success) {
            Fail-Step "Docker tag $($expected.Tag) is missing matrix field: $field"
        }

        $actualValue = $fieldMatch.Groups["value"].Value.Trim()
        if ($actualValue.Length -ge 2) {
            $usesSingleQuotes = $actualValue.StartsWith("'") -and $actualValue.EndsWith("'")
            $usesDoubleQuotes = $actualValue.StartsWith('"') -and $actualValue.EndsWith('"')
            if ($usesSingleQuotes -or $usesDoubleQuotes) {
                $actualValue = $actualValue.Substring(1, $actualValue.Length - 2)
            }
        }
        if ($actualValue -cne [string]$expectedFields[$field]) {
            Fail-Step "Docker tag $($expected.Tag) has wrong $field. Expected '$($expectedFields[$field])', got '$actualValue'."
        }
    }
}

$requiredWorkflowLines = @(
    'platforms: ${{ matrix.platforms }}',
    'VERSION=${{ steps.version.outputs.VERSION }}',
    'PYTHON_RUNTIME_MODE=${{ matrix.python_mode }}',
    'PYTHON_RUNTIME_VERSION=${{ matrix.python_version }}',
    'INSTALL_FULL_TOOLS=${{ matrix.full_tools }}',
    'TAG_CHANNEL: ${{ matrix.tag_channel }}',
    'LEGACY_TAG_CHANNEL: ${{ matrix.legacy_tag_channel }}',
    'VERSION_SUFFIX: ${{ matrix.version_suffix }}',
    'LEGACY_VERSION_SUFFIX: ${{ matrix.legacy_version_suffix }}',
    'IMAGE_REPOSITORY: ${{ env.DOCKER_IMAGE_REPOSITORY }}',
    'echo "$IMAGE_REPOSITORY:$TAG_CHANNEL"',
    'echo "$IMAGE_REPOSITORY:$VERSION$VERSION_SUFFIX"',
    'echo "$IMAGE_REPOSITORY:$LEGACY_TAG_CHANNEL"',
    'echo "$IMAGE_REPOSITORY:$VERSION$LEGACY_VERSION_SUFFIX"',
    'push: true',
    'tags: ${{ steps.docker_tags.outputs.tags }}',
    'cache-from: type=registry,ref=${{ env.DOCKER_IMAGE_REPOSITORY }}:${{ matrix.tag_channel }}'
)
foreach ($job in @(
    [pscustomobject]@{ Name = "Alpine"; Text = $alpineJobText },
    [pscustomobject]@{ Name = "Debian"; Text = $debianJobText }
)) {
    foreach ($requiredLine in $requiredWorkflowLines) {
        if ([regex]::Matches($job.Text, [regex]::Escape($requiredLine)).Count -ne 1) {
            Fail-Step "Docker $($job.Name) job must contain exactly once: $requiredLine"
        }
    }
}

Assert-FileTextContains -Path $workflowPath -Text "DOCKER_IMAGE_REPOSITORY: linzixuanzz/daidai-panel" -Description "official Docker image repository"

foreach ($dockerfileName in @("Dockerfile", "Dockerfile.debian")) {
    $dockerfilePath = Join-Path $repoRoot $dockerfileName
    Assert-FileTextContains -Path $dockerfilePath -Text "ARG PYTHON_RUNTIME_MODE" -Description "$dockerfileName Python runtime mode build arg"
    Assert-FileTextContains -Path $dockerfilePath -Text "ARG PYTHON_RUNTIME_VERSION" -Description "$dockerfileName Python runtime version build arg"
    Assert-FileTextContains -Path $dockerfilePath -Text "ARG INSTALL_FULL_TOOLS" -Description "$dockerfileName full tools build arg"
}

Write-Step "Check Docker Compose update wiring"
foreach ($compose in @(
    [pscustomobject]@{ Name = "docker-compose.yml"; Endpoint = "WATCHTOWER_HTTP_API_ENDPOINTS=update" },
    [pscustomobject]@{ Name = "docker-compose.debian.yml"; Endpoint = "WATCHTOWER_HTTP_API_ENDPOINTS=update" },
    [pscustomobject]@{ Name = "docker-compose.watchtower.prod.yml"; Endpoint = "WATCHTOWER_HTTP_API_ENDPOINTS: update" }
)) {
    $composePath = Join-Path $repoRoot $compose.Name
    if (-not (Test-Path $composePath)) {
        Fail-Step "Missing Docker Compose file: $($compose.Name)"
    }
    $composeText = Get-Content -Path $composePath -Raw -Encoding UTF8
    if ([regex]::Matches($composeText, [regex]::Escape('${DAIDAI_PANEL_IMAGE:-')).Count -ne 2) {
        Fail-Step "$($compose.Name) must use DAIDAI_PANEL_IMAGE for both image and IMAGE_NAME."
    }
    # 这里数的是“赋值处”，不是变量名的出现次数，(?<!\$\{) 这个负向后顾不能删。
    # 这个变量允许用户覆盖（见 .env.watchtower.prod.example），compose 里的写法是
    #     WATCHTOWER_HTTP_API_URL=${WATCHTOWER_HTTP_API_URL:-http://watchtower:8080}
    # 同一行变量名出现两次：一次是被赋值的键，一次是 ${...} 默认值表达式里的引用。
    # 所以裸计数（[regex]::Escape 后直接数原始字符串）恒为 2，门禁必然假阳性 ——
    # db34455 加入覆盖机制时没同步改这条早于它的检查，之后一直没发版才没被发现。
    # 负向后顾把 ${...} 里的引用排除掉，只留赋值处：正常写法计 1；
    # 谁要是又往 watchtower 服务的 environment 里补一份赋值，就会计 2 并照旧 Fail，
    # 原意（该地址只暴露给 panel）不受影响。
    if ([regex]::Matches($composeText, '(?<!\$\{)WATCHTOWER_HTTP_API_URL').Count -ne 1 `
        -or -not $composeText.Contains('http://watchtower:8080')) {
        Fail-Step "$($compose.Name) must expose the stable watchtower service URL only to the panel."
    }
    if (-not $composeText.Contains($compose.Endpoint) -or $composeText.Contains('--http-api-update')) {
        Fail-Step "$($compose.Name) must enable the update endpoint without the deprecated --http-api-update flag."
    }
}

$actionlint = Get-Command actionlint -ErrorAction SilentlyContinue
if ($actionlint) {
    & $actionlint.Source $workflowPath
    if ($LASTEXITCODE -ne 0) {
        Fail-Step "actionlint failed."
    }
} else {
    Write-Host "[WARN] actionlint not found, skip local workflow lint." -ForegroundColor Yellow
}

Write-Step "Check remote tag conflict"
$remoteTagExists = git ls-remote --tags origin ("refs/tags/" + $tagVersion)
if ($LASTEXITCODE -ne 0) {
    Fail-Step "Unable to query remote tags from origin."
}
if ($remoteTagExists) {
    Fail-Step "Remote tag already exists: $tagVersion. Confirm whether you really want to re-release."
}

Write-Step "Check branch status"
$currentBranch = git branch --show-current
if ($LASTEXITCODE -ne 0) {
    Fail-Step "Unable to resolve current git branch."
}
if ($currentBranch -ne "main") {
    Write-Host "[WARN] Current branch is $currentBranch, not main." -ForegroundColor Yellow
}

$aheadBehind = git rev-list --left-right --count origin/main...HEAD
if ($LASTEXITCODE -ne 0) {
    Fail-Step "Unable to compare origin/main with HEAD."
}
Write-Host "origin/main...HEAD = $aheadBehind"

Write-Host ""
Write-Host "[OK] Release preflight passed: $tagVersion" -ForegroundColor Green
