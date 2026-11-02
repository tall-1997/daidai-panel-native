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
# 只校验「注释存在」是不够的：release.yml 那条 grep 曾经因为用了可变长度后顾断言而
# 直接报错、被 `|| true` 吞掉，于是 v3.0.0~v3.2.0 每一版的 Release 标题都静默退化成裸 tag，
# 而这道守卫全程是绿的。所以这里要按 release.yml 的**同一条正则**再抽一次，
# 抽不出非空摘要就判红 —— 守的是「标题真能生成」，不是「注释真存在」。
$releaseNoteText = Get-Content -Path $releaseNotePath -Raw -Encoding UTF8
$titleMatch = [regex]::Match($releaseNoteText, '<!--\s*release-title:\s*(?<summary>.*?)\s*-->')
if (-not $titleMatch.Success -or [string]::IsNullOrWhiteSpace($titleMatch.Groups['summary'].Value)) {
    Fail-Step "release-title marker exists but yields an empty summary; GitHub Release title would fall back to the bare tag."
}
Write-Host ("  release title -> {0}：{1}" -f $tagVersion, $titleMatch.Groups['summary'].Value)
# Release body 里不许出现相对链接。已实测：gh api 取 v3.2.3 的 body，里面是
# [定时任务不执行 / 没有日志](../task-not-running.md)，而 GitHub Release 页面把它渲染成
# href="/linzixuanzz/daidai-panel/blob/task-not-running.md" —— 没有 ref、没有目录，是一个 404。
# 根因是 Release body 没有「源文件路径」这个上下文，`../` 无从解析。
# 所以从 v3.2.5 起本版 notes 一律写绝对 URL。历史文件不动：它们在仓库里点是对的，
# 只是在 Release 页面上坏，改它们等于改历史陈述。
# ⚠️ 先剥掉围栏代码块与行内代码再匹配：发布说明本身会**引用**这种坏写法当反面例子
# （v3.2.5 那篇就是这么写的），包在反引号里的不是一条真链接，判红就是误报。
$scannableNoteText = [regex]::Replace($releaseNoteText, '(?s)```.*?```', '')
$scannableNoteText = [regex]::Replace($scannableNoteText, '`[^`\r\n]*`', '')
$relativeLinks = [regex]::Matches($scannableNoteText, '\]\((?<target>\.{1,2}/[^)]*)\)')
if ($relativeLinks.Count -gt 0) {
    $samples = (($relativeLinks | ForEach-Object { $_.Groups['target'].Value } | Sort-Object -Unique) | Select-Object -First 5) -join ", "
    Fail-Step "Release notes must use absolute URLs. Found relative link target(s): $samples`nA GitHub Release body has no source-file path context, so [x](../task-not-running.md) is rendered as href=`"/linzixuanzz/daidai-panel/blob/task-not-running.md`" - no ref, no directory, a guaranteed 404 (verified against the v3.2.3 release body).`nWrite https://github.com/linzixuanzz/daidai-panel/blob/main/docs/<file> instead. Historical notes are intentionally left alone."
}
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
    # Debian 只发 amd64 / arm64：node:24 基础镜像没有 linux/386 与 linux/arm/v7 manifest，写进去构建必失败。
    [pscustomobject]@{ Job = "debian"; Tag = "debian";       LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "-debian";          LegacySuffix = "";             FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-full";  LegacyTag = "";           Mode = "single"; Python = "3.12"; Suffix = "-debian-full";     LegacySuffix = "";             FullTools = "true";  Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-3.10";  LegacyTag = "debian3.10"; Mode = "single"; Python = "3.10"; Suffix = "-debian-3.10";     LegacySuffix = "-debian3.10";  FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-3.11";  LegacyTag = "debian3.11"; Mode = "single"; Python = "3.11"; Suffix = "-debian-3.11";     LegacySuffix = "-debian3.11";  FullTools = "false"; Platforms = "linux/amd64,linux/arm64" };
    [pscustomobject]@{ Job = "debian"; Tag = "debian-all";   LegacyTag = "debianall";  Mode = "all";    Python = "3.12"; Suffix = "-debian-all";      LegacySuffix = "-debianall";   FullTools = "false"; Platforms = "linux/amd64,linux/arm64" }
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
    # 正向断言（必须含）与负向断言（不得含）拆成两条，不再共用一个 -or 和一句文案。
    # 原来合并写法有两个毛病：一是失败时看不出到底缺了哪半边；二是正向那半边形同虚设 ——
    # 谁误删了 WATCHTOWER_HTTP_API_ENDPOINTS 那一行，读到的仍是一句“deprecated flag”，
    # 与真实原因南辕北辙。
    #
    # 匹配用“非注释行 + 子串”，既不做整行精确匹配，也不用裸 Contains：
    #   - 裸 Contains 会被注释喂饱。compose 里 watchtower 镜像上方写了最低版本说明注释，
    #     正文必然提到 WATCHTOWER_HTTP_API_ENDPOINTS 与 --http-api-update 这两个名字，
    #     于是正向检查删掉真配置也全绿、负向检查没人用那个 flag 也照样红。
    #   - 整行精确匹配则不许行尾追加 YAML 行内注释，多写一句说明就会红。
    # 一律用 -c* 大小写敏感版本：原来的 .Contains() 是 ordinal 比较，环境变量名对 watchtower
    # 也是大小写敏感的，换成默认的 -match 会把 watchtower_http_api_endpoints 这种写法放行。
    $endpointLinePattern = '(?m)^[^#\r\n]*' + [regex]::Escape($compose.Endpoint)
    if ($composeText -cnotmatch $endpointLinePattern) {
        # watchtower 只有读到 WATCHTOWER_HTTP_API_ENDPOINTS 才会启动 HTTP API 服务器并监听 8080。
        # 少了这一行，容器照样正常启动、日志一声不吭，但面板点更新会直接拿到
        # dial tcp ...:8080: connect: connection refused —— 正是 issue #108 的形态。
        Fail-Step "$($compose.Name) must keep '$($compose.Endpoint)' on the watchtower service.`nWithout it watchtower never starts its HTTP API listener on 8080: the container still boots and logs nothing unusual, but every panel update fails with 'connect: connection refused' (issue #108)."
    }
    if ($composeText -cmatch '(?m)^[^#\r\n]*--http-api-update') {
        Fail-Step "$($compose.Name) must not pass the deprecated --http-api-update flag; WATCHTOWER_HTTP_API_ENDPOINTS replaces it and the legacy flag is dropped in watchtower v2."
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
