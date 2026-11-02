package service

import (
	"bytes"
	"debug/elf"
	"encoding/csv"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// pythonLibcMarkerFileName 记录上一次适配托管 venv 时解释器用的 C 库（musl / glibc）。
// 放在 venv 目录里：venv 被隔离重建时标记跟着消失，天然与它描述的 site-packages 同源。
const pythonLibcMarkerFileName = ".daidai-python-libc"

// 平台、ELF 判定与 pip 命令构造抽成包级变量只为单测注入，测试里不读真 ELF、不真跑 pip。
var (
	pythonLibcRepairGOOS           = runtime.GOOS
	pythonInterpreterLibcFunc      = readPythonInterpreterLibc
	elfImportedLibrariesFunc       = readELFImportedLibraries
	newPythonLibcRepairCommandFunc = newPythonLibcRepairCommand
)

// RepairPythonPackagesForLibcChange 在托管 venv 背后的解释器换了 C 库之后（Docker 从 Alpine/musl 换到
// Debian/glibc，或者反过来），按原版本强制重装链接到另一种 C 库的发行包（#150）。
//
// 两种镜像的 Python 装在同一路径、同一版本，数据卷里的 venv 照样能跑：健康检查、启动校验都看不出问题，
// 而 pip install 对「已安装」的包直接跳过，面板上的重装按钮也修不好。只在 Linux 执行，整个过程放后台。
func RepairPythonPackagesForLibcChange() {
	if pythonLibcRepairGOOS != "linux" {
		return
	}
	go func() {
		for _, version := range CurrentPythonRuntimeVersions() {
			repairPythonVenvForLibcChange(version)
		}
	}()
}

// repairPythonVenvForLibcChange 只修确实链接到另一种 C 库的包，能用的一律不碰；依赖记录的 status 一律不改。
func repairPythonVenvForLibcChange(version string) {
	venvDir := ManagedPythonVenvDir(version)
	pythonBin := resolveManagedPythonBinaryInVenv(venvDir)
	sitePackages := findVenvSitePackages(venvDir)
	if pythonBin == "" || sitePackages == "" {
		return
	}
	// 当前 C 库看 venv 背后真实解释器的 PT_INTERP；判不出（静态链接、读不了）就跳过，宁可不修也不误修。
	realBin, err := filepath.EvalSymlinks(pythonBin)
	if err != nil {
		return
	}
	libc := pythonInterpreterLibcFunc(realBin)
	markerPath := filepath.Join(venvDir, pythonLibcMarkerFileName)
	if data, _ := os.ReadFile(markerPath); libc == "" || strings.TrimSpace(string(data)) == libc {
		return
	}

	// 找链接到另一种 C 库的 .so（看 DT_NEEDED）。glibc 下 libc.so 也算外来：musl 工具链编出来的
	// 二进制可能 NEEDED 的就是 libc.so（python-build-standalone 的 musl 解释器就是这样）。
	foreign := map[string]bool{}
	_ = filepath.WalkDir(sitePackages, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".so") {
			return nil
		}
		libs, _ := elfImportedLibrariesFunc(path)
		for _, lib := range libs {
			if (libc == "glibc" && (strings.HasPrefix(lib, "libc.musl-") || lib == "libc.so")) || (libc == "musl" && lib == "libc.so.6") {
				foreign[path] = true
			}
		}
		return nil
	})

	// 按 RECORD 把外来 .so 归到发行包。RECORD 是 CSV，第一列是相对 site-packages 的路径（带逗号时会加引号）。
	// 名字与版本取自目录名 <name>-<version>.dist-info：名字里的 - 已被规范成 _，按最后一个 - 切。
	var requirements []string
	claimed := map[string]bool{}
	records, _ := filepath.Glob(filepath.Join(sitePackages, "*.dist-info", "RECORD"))
	for _, record := range records {
		data, err := os.ReadFile(record)
		if err != nil {
			continue
		}
		reader := csv.NewReader(bytes.NewReader(data))
		reader.FieldsPerRecord = -1
		rows, _ := reader.ReadAll()
		hit := false
		for _, row := range rows {
			if path := filepath.Join(sitePackages, filepath.FromSlash(row[0])); foreign[path] {
				claimed[path], hit = true, true
			}
		}
		distInfo := strings.TrimSuffix(filepath.Base(filepath.Dir(record)), ".dist-info")
		if i := strings.LastIndex(distInfo, "-"); hit && i > 0 {
			requirements = append(requirements, distInfo[:i]+"=="+distInfo[i+1:])
		}
	}
	// 不属于任何 RECORD 的（手工拷进来的、Magisk 快照回填留下的残留）面板不知道该装什么，只记一笔。
	if orphans := len(foreign) - len(claimed); orphans > 0 {
		log.Printf("[Python 依赖] Python %s 环境里有 %d 个按另一种 C 库编译的 .so 不属于任何已安装的包，跳过不处理", version, orphans)
	}

	if len(requirements) > 0 {
		log.Printf("[Python 依赖] Python %s 环境里有 %d 个包按另一种 C 库编译，在当前镜像（%s）上加载不了：%s，后台按原版本重装", version, len(requirements), libc, strings.Join(requirements, "、"))
	}
	// 逐包执行而不是一条命令：pip 一条命令里任何一个包失败，整批都不装。
	failed := false
	for _, requirement := range requirements {
		var output bytes.Buffer
		cmd, err := newPythonLibcRepairCommandFunc(version, requirement)
		if err == nil {
			cmd.Stdout, cmd.Stderr = &output, &output
			err = runNodeABICommand(cmd, DependencyOperationTimeout())
		}
		if err != nil {
			failed = true
			log.Printf("warn: [Python 依赖] 按原版本重装 %s 失败（Python %s）: %v；输出末尾：\n%s", requirement, version, err, tailNodeABIRebuildOutput(output.Bytes()))
		}
	}
	// 有失败就不写标记、下次启动再试：失败多半是开机时网络还没通这类暂时问题，下载或构建失败时旧包原样保留，
	// 不会更糟。而面板上的重装按钮对这种包是空操作，不自动重试用户就没有别的出路。
	if failed {
		log.Printf("warn: [Python 依赖] Python %s 还有包没能重装，下次启动会再试；也可以删除数据目录下的 deps/python/%s 后重启，面板会重建环境并重装已登记的依赖", version, version)
		return
	}
	if len(requirements) > 0 {
		log.Printf("[Python 依赖] Python %s 里链接到另一种 C 库的包已按原版本重装完成：%s", version, strings.Join(requirements, "、"))
	}
	if err := os.WriteFile(markerPath, []byte(libc+"\n"), 0o644); err != nil {
		log.Printf("warn: 写入 Python C 库标记 %s 失败: %v", markerPath, err)
	}
}

// readPythonInterpreterLibc 按解释器的 PT_INTERP（动态加载器路径）判定 C 库，判不出返回空串。
// 不用 DT_NEEDED：musl 版解释器 NEEDED 的是 libc.so 而不是 libc.musl-*；也不看 /lib/ld-musl-* 在不在：
// Debian 上装了 musl 包就会误判成 musl，把所有 glibc 扩展当成外来的反复重装。
func readPythonInterpreterLibc(path string) string {
	f, err := elf.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_INTERP {
			continue
		}
		interp, _ := io.ReadAll(prog.Open())
		switch {
		case strings.Contains(string(interp), "ld-musl-"):
			return "musl"
		case strings.Contains(string(interp), "ld-linux"):
			return "glibc"
		}
	}
	return ""
}

func readELFImportedLibraries(path string) ([]string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ImportedLibraries()
}

// newPythonLibcRepairCommand 构造定点修复命令。这里的 --no-deps 是 pip install 的合法选项：
// 只换这一个包的二进制，不动它的依赖（不加的话 pip 会按最新版重装整棵依赖树）。环境与普通安装一致。
func newPythonLibcRepairCommand(version, requirement string) (*exec.Cmd, error) {
	cmd, err := NewPipInstallCommandForPythonVersionWithFlags(version, requirement, []string{"--force-reinstall", "--no-deps"})
	if err != nil {
		return nil, err
	}
	cmd.Env = append(PipInstallEnv(AppendProxyEnv(os.Environ()), CurrentPipMirror()), "TMPDIR=/tmp")
	return cmd, nil
}
