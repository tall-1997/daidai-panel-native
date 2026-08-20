package service

import (
	"os/exec"
	"strings"
	"testing"
)

// Promise.race 超时兜底：timeout 赢了，被抛弃的 slow() 仍然 pending。
// slow() 背后**没有 handle**（不是 setTimeout），所以事件循环会立刻排空 ——
// 这是这类写法里唯一真正会触发探针的形态。带 setTimeout 的版本进程根本不会提前退出，
// 它会老实等满定时器再走正常路径，探针连触发的机会都没有。
const raceTimeoutScript = `
function slow() { return new Promise(() => {}); }
function timeout(ms) { return new Promise((r) => setTimeout(() => r('timeout'), ms)); }
(async () => {
  const winner = await Promise.race([slow(), timeout(20)]);
  console.log('正常跑完 winner=' + winner);
})();
`

// fire-and-forget 上挂防御性 .catch()：同样是常见写法，脚本正常结束。
const catchGuardScript = `
function report() { return new Promise(() => {}); }
(async () => {
  report().catch(() => {});
  await new Promise((r) => setTimeout(r, 20));
  console.log('正常跑完');
})();
`

// 顶层裸 fire-and-forget，没人 await 也没人 catch。
const bareFireForgetScript = `
new Promise(() => {});
(async () => {
  await new Promise((r) => setTimeout(r, 20));
  console.log('正常跑完');
})();
`

// 这三条是「脚本正常结束但留下未完成 Promise」的写法。
//
// **探针在这三种下都会报，而且这是设计上无法消除的**：被抛弃的 promise 与被卡住的
// promise 在依赖图上完全同构，只有脚本自己知道工作做没做完。调阈值也不行 ——
// 实测真阳性（TestSilentExitProbeCatchesHangingPromise）的 pending 数同样可以是 1。
//
// 所以本用例锁的不是「不报」，而是**三条豁免通道每一条都真的能关掉它**。
// 这些通道是误报用户唯一的出路，任何一条失效都必须让 CI 红。
func TestSilentExitProbeExemptionsWorkOnCommonIdioms(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	idioms := []struct {
		name   string
		script string
	}{
		{"Promise.race 超时兜底", raceTimeoutScript},
		{"fire-and-forget 挂 catch 守卫", catchGuardScript},
		{"顶层裸 fire-and-forget", bareFireForgetScript},
	}

	for _, tc := range idioms {
		// 先确认这三条确实会被报 —— 否则下面的豁免用例是在测空气。
		t.Run(tc.name+"/未豁免时确实会报", func(t *testing.T) {
			out, exitCode := runNodeWithProbe(t, nodeBin, tc.script, true)
			if !strings.Contains(out, "正常跑完") {
				t.Fatalf("测试脚本本身没跑通，output=%q", out)
			}
			if !strings.Contains(out, "[任务疑似半路结束]") || exitCode != 75 {
				t.Fatalf("前提不成立：这条惯用法没有触发探针，exit=%d output=%q", exitCode, out)
			}
		})

		// 通道一：脚本末尾声明「我跑完了」。
		t.Run(tc.name+"/daidaiDone 可豁免", func(t *testing.T) {
			out, exitCode := runNodeWithProbe(t, nodeBin, tc.script+"\nglobalThis.daidaiDone?.();\n", true)
			if strings.Contains(out, "[任务疑似半路结束]") || exitCode != 0 {
				t.Fatalf("daidaiDone 没能豁免，exit=%d output=%q", exitCode, out)
			}
		})

		// 通道二：给这个任务单独配环境变量关掉。
		t.Run(tc.name+"/任务级环境变量可豁免", func(t *testing.T) {
			out, exitCode := runNodeWithProbeEnv(t, nodeBin, tc.script, true, []string{"DAIDAI_SILENT_EXIT_DETECT=0"})
			if strings.Contains(out, "[任务疑似半路结束]") || exitCode != 0 {
				t.Fatalf("任务级开关没能豁免，exit=%d output=%q", exitCode, out)
			}
		})
	}
}

// daidaiDone 挂在 globalThis 上，脚本里如果出现同名赋值不能把脚本打断。
func TestSilentExitProbeDaidaiDoneIsNotBrittle(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	// configurable:true 才允许被重定义；若写成 false，这行赋值会抛 TypeError。
	script := `
globalThis.daidaiDone = function () {};
console.log('脚本没被打断');
`
	out, exitCode := runNodeWithProbe(t, nodeBin, script, true)
	if !strings.Contains(out, "脚本没被打断") || exitCode != 0 {
		t.Fatalf("同名赋值把脚本打断了，exit=%d output=%q", exitCode, out)
	}
}

// --require 会被 child_process.fork 继承，子进程不能重复武装、重复刷告警。
func TestSilentExitProbeDoesNotArmTwiceInForkedChild(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	script := `
const { fork } = require('child_process');
const path = require('path');
const fs = require('fs');
// 子进程脚本：故意留一个永不完成的 promise。若探针在子进程里被二次武装，
// 这里会打出第二份告警，父子两份一起进同一条日志。
const childFile = path.join(__dirname, 'child.js');
fs.writeFileSync(childFile, 'new Promise(() => {});\nconsole.log("子进程跑完");\n');
const child = fork(childFile, [], { stdio: 'inherit' });
child.on('exit', () => { console.log('父进程跑完'); });
globalThis.daidaiDone?.();
`
	out, exitCode := runNodeWithProbe(t, nodeBin, script, true)
	if !strings.Contains(out, "子进程跑完") || !strings.Contains(out, "父进程跑完") {
		t.Fatalf("fork 用例本身没跑通，exit=%d output=%q", exitCode, out)
	}
	if n := strings.Count(out, "[任务疑似半路结束]"); n != 0 {
		t.Fatalf("子进程被重复武装，刷出了 %d 行告警，output=%q", n, out)
	}
}
