package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docker/entrypoint.sh 在整个仓库里没有任何测试、也没有任何 CI 步骤引用它 ——
// 改坏了 go test 照样全绿。下面这几条是纯静态字符串断言，
// **防不住 shell 逻辑写错**，只能防住关键行被删掉 / 改回旧写法，
// 真正的验证仍然是「docker build + 带 PUID 起一次容器 + 在面板里装一个依赖」。
//
// 之所以放在 service 包：这里锁的是 entrypoint 与 dependency_home.go 之间的跨层契约 ——
// 两边必须指向同一个 HOME，否则会变成「entrypoint 建在 A、代码写到 B」。

func readDockerEntrypoint(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docker", "entrypoint.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docker/entrypoint.sh: %v", err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// entrypoint 建的 HOME 必须与 EffectiveHomeDir 的回落目录是同一个。
// 这条是本次修复里唯一的跨层契约，两边不一致时两层修复都还在、却互相看不见。
func TestDockerEntrypointHomeMatchesGoFallback(t *testing.T) {
	text := readDockerEntrypoint(t)

	const shellHome = `DAIDAI_HOME="${DATA_DIR}/.home"`
	if !strings.Contains(text, shellHome) {
		t.Fatalf("entrypoint.sh 必须把降权用户的 HOME 设在 %q", shellHome)
	}

	// Go 侧的回落路径由 resolveWritableHome 拼出来，这里反向确认它就是 <dataDir>/.home。
	// 用纯逻辑函数而不是 EffectiveHomeDir：后者在 Windows 上会短路返回，
	// 那样这条跨层契约在开发机上就成了永远为真的空断言。
	dataDir := t.TempDir()
	missing := filepath.Join(dataDir, "never-created-home")
	if got := resolveWritableHome(missing, dataDir); got != filepath.Join(dataDir, ".home") {
		t.Fatalf("Go 侧回落目录与 entrypoint 的 DAIDAI_HOME 不一致，got=%q", got)
	}
}

// 降权拉起面板时必须用 env 显式钉 HOME。
// su-exec 会按 /etc/passwd 覆写 HOME，gosu 只在 HOME 为空时才设置（Docker 默认已注入
// HOME=/root），两个工具行为不一致；只靠 passwd 里的家目录字段，Debian 那条路修不好。
func TestDockerEntrypointPinsHomeWhenDroppingPrivilege(t *testing.T) {
	text := readDockerEntrypoint(t)

	for _, snippet := range []string{
		`su-exec "${RUN_AS_SPEC}" /usr/bin/env "HOME=${DAIDAI_HOME}" /app/daidai-server`,
		`gosu "${RUN_AS_SPEC}" /usr/bin/env "HOME=${DAIDAI_HOME}" /app/daidai-server`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("entrypoint.sh 缺少显式钉 HOME 的启动方式: %q", snippet)
		}
	}

	// env 必须写绝对路径：上面 export 的 PATH 首位是
	// ${DATA_DIR}/deps/nodejs/node_modules/.bin，那是面板用户可写的目录。
	// 用户装到一个 bin 名恰好叫 env 的 npm 包就会把它劫持掉，
	// daidai-server 根本不会被执行，表现成「容器每 2 秒重启、日志只有一个退出码」。
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, ` env "HOME=`) && !strings.Contains(trimmed, `/usr/bin/env "HOME=`) {
			t.Fatalf("entrypoint.sh:%d 的 env 必须写绝对路径 /usr/bin/env（PATH 首位是面板用户可写目录）: %s", i+1, trimmed)
		}
	}

	// 降权时必须把 gid 一起传给 su-exec / gosu。只传用户名的话，
	// 复用现成账号那条路上两个工具都会按 passwd 里的主组取 gid，PGID 被静默丢掉。
	if !strings.Contains(text, `RUN_AS_SPEC="${TARGET_USER}:${TARGET_GID}"`) {
		t.Fatal("entrypoint.sh 必须以 user:group 形式降权，否则 PGID 会被静默忽略")
	}

	// 不降权时必须还是那条裸启动，历史部署逐字节不变。
	if !strings.Contains(text, "    /app/daidai-server &") {
		t.Fatal("entrypoint.sh 必须保留不降权时的裸启动分支")
	}
}

// 建用户不能再是「声明了家目录却从不创建」：那正是 npm 报
// EACCES: mkdir '/home/daidai' 的直接原因。
func TestDockerEntrypointCreatesHomeForRunUser(t *testing.T) {
	text := readDockerEntrypoint(t)

	// 必须带兜底：数据目录以 :ro 挂载、或 NFS root_squash 把容器内 root 压成 nobody 时
	// 这句会返回 EACCES，裸写会被 set -e 直接带出 —— docker logs 里一行输出都没有，
	// 而后面那道可写性预检本来能给出「数据目录不可写 + 三条原因 + 修复命令」。
	if !strings.Contains(text, `mkdir -p "${DAIDAI_HOME}" 2>/dev/null || true`) {
		t.Fatal("entrypoint.sh 创建 HOME 的那句必须带 || true，否则只读数据目录下会被 set -e 静默带出")
	}
	// HOME 在 DATA_DIR 下，靠这条 chown -R 一并覆盖属主。
	if !strings.Contains(text, `chown -R "${TARGET_UID}:${TARGET_GID}" "${DATA_DIR}" /tmp`) {
		t.Fatal("entrypoint.sh 必须保留对数据目录的 chown（HOME 就在它下面）")
	}
	// 建用户时家目录字段要指向同一个位置，su-exec 覆写 HOME 时才落在可写目录上。
	for _, snippet := range []string{
		`-d "${DAIDAI_HOME}"`,
		`-h "${DAIDAI_HOME}"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("建用户时必须指定家目录 %q（shadow 与 busybox 的参数名不同，两条都要有）", snippet)
		}
	}
	// 旧写法：既不建家目录、也不指定家目录。留着就等于 bug 回来了。
	for _, forbidden := range []string{
		`adduser -D -H -u "${TARGET_UID}" -G daidai daidai`,
		`useradd -M -u "${TARGET_UID}" -g "${TARGET_GID}" -s /sbin/nologin daidai`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("entrypoint.sh 不应残留不指定家目录的旧建用户写法: %q", forbidden)
		}
	}
}

// 建组 / 建用户在 UID/GID 撞车时必须能兜住。
// Debian 镜像基于 node:24-bookworm-slim，自带 uid/gid 1000 的 node 用户，
// 而 compose 注释里给的示例恰好就是最常见的 PUID=1000 / PGID=1000 ——
// 原来那几行末尾没有兜底，set -e 会把整个容器带崩，用户只看到「容器起不来」。
func TestDockerEntrypointSurvivesUidGidCollision(t *testing.T) {
	text := readDockerEntrypoint(t)

	for _, snippet := range []string{
		// 先查现成的组 / 用户，撞车就直接复用
		`TARGET_GROUP=$(getent group "${TARGET_GID}" 2>/dev/null | cut -d: -f1)`,
		`TARGET_USER=$(getent passwd "${TARGET_UID}" 2>/dev/null | cut -d: -f1)`,
		// PUID/PGID 改过之后只做 docker restart 时，容器层里的旧账号要就地改而不是重建
		`groupmod -g "${TARGET_GID}" daidai`,
		`usermod -u "${TARGET_UID}" -g "${TARGET_GID}" -d "${DAIDAI_HOME}" daidai`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("entrypoint.sh 缺少 UID/GID 撞车处理片段: %q", snippet)
		}
	}

	// 建组 / 建用户的每一行都必须带兜底，任何一条裸奔都会在 set -e 下带崩容器。
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		// 注释、以及 `command -v groupadd` 这类只是探测存在性的行都不算真正的建组/建用户
		if strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, "command -v") {
			continue
		}
		hasCreateCmd := false
		for _, cmd := range []string{"groupadd ", "addgroup ", "useradd ", "adduser ", "usermod ", "groupmod "} {
			if strings.Contains(trimmed, cmd) {
				hasCreateCmd = true
				break
			}
		}
		if !hasCreateCmd {
			continue
		}
		if strings.HasSuffix(trimmed, "|| \\") || strings.HasSuffix(trimmed, "|| true") {
			continue
		}
		t.Fatalf("entrypoint.sh:%d 建组/建用户的命令必须带兜底（set -e 下失败会直接带崩容器）: %s", i+1, trimmed)
	}
}

// 青龙兼容层（#110）在容器里必须由 entrypoint 以 root 身份建：
// 配了 PUID 之后 daidai-server 是降权跑的，Go 侧 MkdirAll("/ql") 必然 EACCES 静默失败。
// 这条同样只是静态字符串断言，防的是「这一段被顺手删掉 / 改回旧写法」。
func TestDockerEntrypointCreatesQingLongCompatLayout(t *testing.T) {
	text := readDockerEntrypoint(t)

	// 必须在 PUID 守卫之前：不设 PUID 的默认 root 部署同样需要 /ql。
	guardIdx := strings.Index(text, `if [ -n "${PUID}" ] || [ -n "${PGID}" ]; then`)
	if guardIdx < 0 {
		t.Fatal("entrypoint.sh 找不到 PUID/PGID 的 opt-in 守卫")
	}
	mkdirIdx := strings.Index(text, `mkdir -p /ql/shell /ql/data`)
	if mkdirIdx < 0 {
		t.Fatal("entrypoint.sh 必须创建 /ql/shell 与 /ql/data 两个真实目录")
	}
	if mkdirIdx > guardIdx {
		t.Fatalf("/ql 兼容层必须建在 PUID 守卫之前，否则默认 root 部署拿不到 (idx=%d guard=%d)", mkdirIdx, guardIdx)
	}

	// 软链映射表必须与 Go 侧 ensureQingLongCompatLayoutAt 一一对应，
	// 少一条就是「面板里能跑的青龙脚本，换个部署方式就跑不了」。
	for _, snippet := range []string{
		`link_ql_path /ql/data/repo "${DATA_DIR}/scripts"`,
		`link_ql_path /ql/data/scripts "${DATA_DIR}/scripts"`,
		`link_ql_path /ql/scripts "${DATA_DIR}/scripts"`,
		`link_ql_path /ql/data/log "${DATA_DIR}/logs"`,
		`link_ql_path /ql/log "${DATA_DIR}/logs"`,
		`link_ql_path /ql/data/config "${DATA_DIR}"`,
		`link_ql_path /ql/config "${DATA_DIR}"`,
		`link_ql_path /ql/data/deps "${DATA_DIR}/deps"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("entrypoint.sh 缺少青龙兼容软链: %q", snippet)
		}
	}

	// 青龙脚本的第一处报错点就是 touch "$dir_shell/env.sh"。
	if !strings.Contains(text, `[ -f /ql/shell/env.sh ] || touch /ql/shell/env.sh`) {
		t.Fatal("entrypoint.sh 必须准备 /ql/shell/env.sh 空占位文件（青龙脚本第一步就 touch 它）")
	}

	// link_ql_path 必须先判「已经是真实目录就不动」：对真实目录执行 ln -sfn
	// 会把链接建到那个目录**里面**去（用户把自己的青龙 /ql 挂进容器时就会撞上）。
	if !strings.Contains(text, `if [ -e "$1" ] && [ ! -L "$1" ]; then`) {
		t.Fatal("link_ql_path 必须跳过已经是真实路径的位置，否则 ln 会把链接建进目录内部")
	}

	// 降权时 /ql 也要交给运行用户，否则青龙脚本 touch env.sh 会 EACCES。
	// 🔴 但必须是【非递归】、且【只在 /ql 是我们自己建出来时】才做：
	// 用户可以把宿主机上真实的青龙 /ql 挂进容器（link_ql_path 的注释就设想了这个场景），
	// 对它 chown -R 会递归改写人家整棵青龙数据的属主，而这里带着 `|| true`，一个字都不会报。
	// 这条断言就是防「后人图省事又把 /ql 塞回上面那条 chown -R 里」。
	if !strings.Contains(text, `chown "${TARGET_UID}:${TARGET_GID}" /ql /ql/shell /ql/data /ql/shell/env.sh`) {
		t.Fatal("降权时必须把 /ql 的三个真实节点 chown 给运行用户（非递归），否则青龙脚本写不进 /ql/shell/env.sh")
	}
	if strings.Contains(text, `chown -R "${TARGET_UID}:${TARGET_GID}" "${DATA_DIR}" /tmp /ql`) {
		t.Fatal("不能对 /ql 做 chown -R：用户挂载真实青龙 /ql 时会递归改写整棵青龙数据的属主")
	}
	if !strings.Contains(text, `if [ "${QL_PREEXISTING}" = "0" ]; then`) {
		t.Fatal("必须用 QL_PREEXISTING 判据把「用户自己挂进来的 /ql」排除在 chown 之外")
	}
	// 判据必须是「存在且非空」：只判 `[ -e /ql ]` 会被 tmpfs 挂载点、
	// 编排预创建的空目录误伤，结果是我们自己建的 /ql/shell/env.sh 也不 chown，
	// 降权后的青龙脚本第一步 touch 就 EACCES（这条正是回归脚本实测抓到过的）。
	if !strings.Contains(text, `if [ -d /ql ] && [ -n "$(ls -A /ql 2>/dev/null)" ]; then`) {
		t.Fatal("QL_PREEXISTING 必须按「存在且非空」判定，只判存在会把空挂载点也当成用户数据")
	}
}

// 只设 PGID 不设 PUID 时 TARGET_UID 会取到 0，
// 原来会造出一个 uid=0 的假 daidai：看起来降了权、实际仍是 root。
func TestDockerEntrypointRejectsRootPuid(t *testing.T) {
	text := readDockerEntrypoint(t)

	if !strings.Contains(text, `if [ "${TARGET_UID}" = "0" ]; then`) {
		t.Fatal("entrypoint.sh 必须显式处理 PUID=0 / 只设 PGID 的情况")
	}
	if !strings.Contains(text, "等价于以 root 运行，已跳过降权") {
		t.Fatal("PUID=0 时必须给出中文说明，而不是静默造一个假降权用户")
	}
}

// 所有降权相关改动都必须落在 PUID/PGID 的 opt-in 守卫内，
// 不设 PUID 的历史部署（默认 root）必须逐字节零影响。
func TestDockerEntrypointKeepsPrivilegeChangesOptIn(t *testing.T) {
	text := readDockerEntrypoint(t)

	guardIdx := strings.Index(text, `if [ -n "${PUID}" ] || [ -n "${PGID}" ]; then`)
	if guardIdx < 0 {
		t.Fatal("entrypoint.sh 找不到 PUID/PGID 的 opt-in 守卫")
	}
	// 两个状态变量必须在守卫之前初始化为空，否则不设 PUID 时会引用到未定义变量。
	for _, snippet := range []string{`RUN_AS_USER=""`, `DAIDAI_HOME=""`} {
		idx := strings.Index(text, snippet)
		if idx < 0 || idx > guardIdx {
			t.Fatalf("%q 必须在 PUID 守卫之前初始化 (idx=%d guard=%d)", snippet, idx, guardIdx)
		}
	}
	for _, snippet := range []string{
		`DAIDAI_HOME="${DATA_DIR}/.home"`,
		`mkdir -p "${DAIDAI_HOME}"`,
		`chown -R "${TARGET_UID}:${TARGET_GID}" "${DATA_DIR}" /tmp`,
	} {
		if idx := strings.Index(text, snippet); idx < guardIdx {
			t.Fatalf("%q 跑到了 PUID 守卫之外，会影响默认 root 部署 (idx=%d guard=%d)", snippet, idx, guardIdx)
		}
	}
}
