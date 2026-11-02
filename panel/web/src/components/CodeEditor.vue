<script setup lang="ts">
import {
  computed,
  defineAsyncComponent,
  onBeforeUnmount,
  onMounted,
  provide,
  ref,
  shallowRef,
} from "vue";
import CodeMirrorEditor from "./CodeMirrorEditor.vue";
import {
  EDITOR_ENGINE_CHANGE_EVENT,
  readStoredEditorEngine,
  resolveEditorEngine,
  type ResolvedEditorEngine,
} from "@/utils/editorEngine";
import {
  MONACO_LOADING_DELAY_MS,
  MONACO_LOAD_RETRY_KEY,
  MonacoEditorLoadError,
  MonacoEditorLoading,
} from "@/utils/monacoWarmup";
// Monaco 失败态里的「重试 / 改用 CodeMirror」是渲染函数里的 ElButton，不经过 unplugin-vue-components，样式要自己引。
// 引在这里而不是 monacoWarmup.ts：那个文件在首屏 chunk 里，会把整份按钮样式搬进首屏 CSS（见那边的注释）；
// 本文件在懒加载 chunk 里，调用页本来就用着 el-button，这一句不会多下载任何东西。
import "element-plus/theme-chalk/el-button.css";

/**
 * 代码编辑器（引擎分发层）。
 *
 * 本组件自己**不渲染任何编辑器**，只负责按用户的引擎偏好在两份实现之间二选一：
 *   - CodeMirrorEditor.vue：默认引擎，也是**移动端唯一**引擎。内容区是 contenteditable、
 *     选区就是原生 Selection，「长按出系统菜单 / 双击出选择手柄 / 按住拖动光标」天然可用。
 *   - MonacoEditor.vue：桌面端可选。多光标、列选、JSON 语言服务那套更强，
 *     但它是自绘编辑器，上面三条**无论怎么配都做不到**。
 * 两份实现的 props / emit / defineExpose / 根 class 逐字相同（唯一的例外是 minimap，
 * 理由写在 CodeMirrorEditor.vue 那个 prop 的注释里），调用点感知不到拿到的是哪一个。
 * 注意那条契约约束的是**两份实现之间**：本分发层的 emit 面是它们的超集
 * （多一个 update:modelValue 的转发、多一个 engine-resolved 的上报），详见下面 emits 的注释。
 *
 * 📦 v3.2.4 起本文件**不再是偏好的家**。
 * 这里原本有一个普通 `<script>` 块，导出 6 个具名成员（自动换行一组、三个视图开关一组
 * 「类型 + 读 + 写」，纯 localStorage）。issue #116 要求偏好「换设备 / 换 IP 也记得住」，
 * 于是它们整体搬到了 utils/editorPreferences.ts —— 那边多了一条服务端同步链路
 * （/api/auth/preferences），而**组件文件不是发请求的地方**。
 * 顺带解决的还有：新加的 indent_width 是三态/五态而不是布尔，旧的
 * readStoredEditorViewOption(name): boolean 那个签名装不下。
 * 两个页面（脚本页 / 配置文件页）现在从 utils/editorPreferences.ts 引入，不再从本文件引入。
 * 🔴 所以**不要**再往本文件加具名导出：普通 `<script>` 块一旦回来，
 * 「偏好只有一处真源」这条又会开始漂。
 *
 * 两个引擎实现**不从这里 import 任何东西**（偏好的值是页面读出来、当 prop 传进去的），
 * 所以「分发层 import 实现、实现 import 分发层」的循环依赖不存在。
 *
 * 🔴 模板是**单个** `<component :is>` 根节点，不套 wrapper `<div>`、也不在根上放注释
 * （dev 构建会保留注释节点，根就变成 Fragment，透传要靠 DEV_ROOT_FRAGMENT 那套 dev 专用机制兜，
 * 平白让 dev 与 prod 走两条不同的路径）。理由：
 *   - 三个调用点靠 $attrs 穿透给编辑器根节点加样式 —— ScriptsEditorPane 传
 *     `class="code-editor"`（scoped 规则 `.code-editor { flex: 1; height: 100%; min-height: 0 }`），
 *     调试弹窗与代码运行器传内联 `style="height: 100%; min-height: 0"`。
 *     套一层 div 之后这些 class / style 会落在那层 div 上，真正的编辑器根节点一点样式都拿不到，
 *     表现是编辑器高度塌陷，而构建与类型检查全绿。
 *   - 同理本文件刻意**没有** `<style>` 块：scoped 样式只能命中子组件的根节点，
 *     `.code-editor-container` 和那几条 `:deep(.cm-*)` 都命中不到，它们必须跟着各自的实现走。
 *
 * 模板里 props 一个个显式列出而不是 `v-bind="props"`：逐个列 vue-tsc 才能逐个查类型，
 * 一把梭传下去的话，两边 prop 名写歪了也发现不了。
 */

// Monaco 用异步组件引入，这是「不切过去的用户一个字节都不下载」的落点：
// 静态 import 会让 Rollup 把 MB 级的 monaco chunk 提升成入口的静态依赖，
// 构建全绿、页面能用，纯静默劣化。CodeMirror 是默认引擎、绝大多数会话都要用，静态引入即可。
//
// v3.2.8（#133 / #126）起带加载态与失败态，推翻了原来「刻意不给 loadingComponent」的取舍：
//   - 原来的理由是「同源本地资源，一帧内解析完」。实际上二进制 / Magisk 部署由 Go 直出、此前既没有
//     缓存头也没有压缩，第一次进编辑页要现下现解析约 4 MB，最长白屏 4 秒。
//   - 加载占位延迟 MONACO_LOADING_DELAY_MS（200ms）才露出来：有缓存时照旧直接出编辑器，不闪一下。
//   - 原来 chunk 一失败（升级后的旧页面、网络抖动）编辑区就永远空白，没有提示也没有出路；
//     现在失败态给「重试」与「改用 CodeMirror」。页面级「自动刷新一次」在 main.ts 的 vite:preloadError 里，
//     这里的失败态是它被限次拦下（60 秒内刚刷过）之后的兜底。
//   - 占位组件拿到的是与编辑器同一批 props 与 class / style（Vue 异步组件就是这么转的），
//     尺寸按同一套 minHeight / fillHeight 规则算，编辑器挂上来时不跳（见 utils/monacoWarmup.ts）。
//
// loader 把 MonacoEditor.vue 与 monacoEngine 两个 chunk 一起拉：MonacoEditor.vue 本身只有几 KB，
// 真正的大头（Monaco 本体）是它挂载后才 await import 的 monacoEngine。只等前者的话，加载占位一闪就换成
// 一块空白编辑区，剩下的几秒照样白屏，而且两个 chunk 是先后串行下载的。一起拉之后占位覆盖整个等待，
// MonacoEditor 挂上去时 monacoEngine 已在模块表里，它那一发 import 当场 resolve。
// 两个都仍是动态 import，「不切过去的人一个字节都不下载」不变。
//
// 「重试」换一个全新的异步组件定义：包装层进了 error 态就一直停在那里，换定义等于让 Vue 卸掉旧包装层、
// 挂一个带自己加载状态的新包装层，loader 必然重跑一遍。重试函数经 provide 交给 errorComponent
// （Vue 创建它时只给一个 error prop，够不着这里的函数）。
function createMonacoEditorComponent() {
  return defineAsyncComponent({
    loader: () =>
      Promise.all([
        import("./MonacoEditor.vue"),
        import("@/utils/monacoEngine"),
      ]).then(([editorModule]) => editorModule),
    loadingComponent: MonacoEditorLoading,
    delay: MONACO_LOADING_DELAY_MS,
    errorComponent: MonacoEditorLoadError,
  });
}
const MonacoEditor = shallowRef(createMonacoEditorComponent());
provide(MONACO_LOAD_RETRY_KEY, () => {
  MonacoEditor.value = createMonacoEditorComponent();
});

// ⚠️ props 必须与两份实现**逐字相同**，并且下面模板里要一个个显式传下去。
const props = withDefaults(
  defineProps<{
    modelValue: string;
    language?: string;
    readonly?: boolean;
    minHeight?: string | number;
    /**
     * 撑满父容器高度。
     * 开启后外层容器改为 `flex: 1 1 auto; height: 100%`，高度由父级 flex 链决定，
     * `min-height` 退化为「下限」（默认 0，父级没有确定高度时可由调用方另行给下限）。
     */
    fillHeight?: boolean;
    /**
     * 自动换行。默认 'on'，与改造前一致，
     * 不传该 prop 的调用点（调试弹窗 / 代码运行器）行为零变化。
     */
    wordWrap?: "on" | "off";
    /**
     * 右侧代码缩略图。默认 false。
     * ⚠️ 下面几个 prop 的默认值一律等于「加这些功能之前的行为」，
     * 所以不传它们的调用点（调试弹窗 / 代码运行器）一个像素都不会变。
     * 脚本页与配置文件页会显式传各自记住的偏好。
     *
     * 🔴 v3.2.4 起**只有 Monaco 真的实现它**：CodeMirror 侧那份自绘缩略图已经删掉，
     * 那边同名 prop 是刻意留空的 no-op（理由见 CodeMirrorEditor.vue 的注释）。
     * 这里仍然照常往下传 —— 换成「只在 monaco 分支才 v-bind 上去」的写法，
     * 就等于放弃了下面模板逐个显式传时 vue-tsc 的逐项类型检查，不划算。
     * 菜单里那一项该不该显示由**页面**按当前引擎决定，不在本层过滤。
     */
    minimap?: boolean;
    /** 缩进参考线。默认 false（同上，偏好本身的默认值是开，由页面侧决定） */
    indentGuides?: boolean;
    /**
     * 显示空白符三态（issue #116-4）。默认 'none'。
     *   - 'all'：一直画（空格灰点、Tab 箭头）
     *   - 'selection'：只画选中范围内的
     *   - 'none'：不画
     * 取代了 v3.2.2 的布尔 showWhitespace：那个二态装不下用户真正想要的「仅选中」。
     */
    whitespace?: "none" | "selection" | "all";
    /**
     * 缩进宽度（issue #116-1/3）。默认 2，等于加这个功能之前两个引擎写死的值。
     * 'auto' 表示按文档内容检测，页面侧的偏好默认就是 'auto'。
     */
    indentWidth?: "auto" | number;
  }>(),
  {
    wordWrap: "on",
    minimap: false,
    indentGuides: false,
    whitespace: "none",
    indentWidth: 2,
  },
);

// emit 必须显式声明：不声明的话 update:modelValue 会退化成透传的 $attrs 事件，
// 调用点 v-model 的类型检查当场失效（值类型变成 any，写错了也查不出来）。
const emit = defineEmits<{
  "update:modelValue": [value: string];
  /**
   * 把「本次真正用上的引擎」上报给页面（issue #117）。
   * 值的类型就是 ResolvedEditorEngine，即 'codemirror' | 'monaco'。
   * 触发时机与本层解析引擎的时机一一对应：挂载时一次，
   * EDITOR_ENGINE_CHANGE_EVENT 触发 syncEngine() 重新解析后一次。
   *
   * 🔴 这一条**只加在分发层**，两个引擎实现里都不加。
   * 「两份实现的 props / emit / defineExpose 逐字相同」那条契约约束的是**引擎实现之间**——
   * 它们是同一个契约的两份实现，谁多一个事件都会让调用点在切引擎时行为分叉。
   * 而分发层本来就比它们多一层职责（它已经多了一个 update:modelValue 的转发），
   * 并且「当前用的是哪一个引擎」这件事**只有它知道**：引擎实现自己拿不到偏好，
   * 也无从判断自己是被 auto 选中的还是被显式选中的，让它们来报反倒要多传一份信息进去。
   */
  "engine-resolved": [engine: ResolvedEditorEngine];
}>();

/**
 * 两个引擎实现共同暴露的方法集，与它们各自 defineExpose 的四个键逐字对应。
 * 成员写成可选：Monaco 还在加载时，Vue 会把模板 ref 一并转给 loadingComponent（runtime-core 的
 * createInnerComp 连 ref 带 props 一起转），这时 engineRef 指着的是加载占位，身上没有这四个方法。
 */
interface EditorEngineExposed {
  focus?: () => void;
  getValue?: () => string;
  setValue?: (value: string) => void;
  format?: () => void;
}

const engineRef = ref<EditorEngineExposed | null>(null);

// 挂载时解析一次。auto 的设备判定（coarse pointer / 窄屏硬回落 CodeMirror）在
// utils/editorEngine.ts 里，本文件不重复一份判断。
const engine = ref(resolveEditorEngine(readStoredEditorEngine()));

// 菜单里切引擎 → persistEditorEngine 派发 EDITOR_ENGINE_CHANGE_EVENT → 这里重新解析。
// 这就是「切一下，已经开着的编辑器跟着换」的全部机制；少了它的表现是
// 「菜单里选了、当前这个编辑器纹丝不动，要刷新页面才生效」，不报错。
// 重新读一遍 localStorage 而不是从事件里取值：偏好可能是在另一个组件里改的，
// 存储才是唯一真源。
function syncEngine() {
  engine.value = resolveEditorEngine(readStoredEditorEngine());
  // 解析完立刻上报，理由见 onMounted 里那次上报的大段注释。
  // 刻意**不**先比较新旧值：页面侧收到的是「当前实际引擎」的一次断言，
  // 重复上报只是给同一个 ref 赋同一个值（同值赋值不会触发更新），
  // 而漏报一次的代价是菜单和正文重新分叉，两边不对等。
  emit("engine-resolved", engine.value);
}

onMounted(() => {
  window.addEventListener(EDITOR_ENGINE_CHANGE_EVENT, syncEngine);
  // 挂载时把解析结果报给页面。这是 issue #117「菜单里有这一项、点了没反应、按钮还高亮」
  // 的修复主线，别当成一次多余的通知删掉：
  //   - 页面侧的 resolvedEngine 是 computed(() => resolveEditorEngine(editorEngine.value))，
  //     依赖只有偏好这一个 ref；而 resolveEditorEngine('auto') 内部读的
  //     window.matchMedia('(pointer: coarse)') 与 window.innerWidth **都不是响应式的**，
  //     偏好没变时那个 computed 求值一次就一直吃缓存。
  //   - 本组件却是**每次挂载**重新解析的，而两个页面的编辑器都挂在 v-if 上
  //     （配置文件页 v-if="!loading"，点一次「刷新」就整块重建；脚本页还有 v-if="isBinary"），
  //     于是两者会分叉：宽窗口下打开页面（菜单里有「代码缩略图」）→ 把窗口贴靠成半屏
  //     使 innerWidth ≤ 1024 → 点「刷新」→ 编辑器重建成了 CodeMirror，而菜单里那一项还在，
  //     点它就是开关翻成 ON、正文毫无变化。反过来（窄 → 宽）则是本次会话再也没有入口开缩略图。
  // 让分发层把真正用上的引擎报回去，页面就不必自己再猜一次。
  emit("engine-resolved", engine.value);
});

onBeforeUnmount(() => {
  window.removeEventListener(EDITOR_ENGINE_CHANGE_EVENT, syncEngine);
});

// 引擎变了 → 组件类型变了 → Vue 会卸载旧实例、挂载新实例（不用额外给 key）。
// 撤销历史与滚动位置会丢，这是换引擎不可避免的代价，也符合用户「我要换一个编辑器」的预期。
// MonacoEditor 是 shallowRef（「重试」会换成新的异步组件定义），这里取 .value，computed 才跟得上换定义
const engineComponent = computed(() =>
  engine.value === "monaco" ? MonacoEditor.value : CodeMirrorEditor,
);

// 显式转发而不是让 v-model 事件走 $attrs：上面声明了 emits，事件就不在 $attrs 里了。
// 写成具名函数（而不是模板里的 `emit('update:modelValue', $event)`）是为了给 $event 一个
// 确定的类型 —— 动态组件上的事件参数会被推成隐式 any。
function onInnerUpdate(value: string) {
  emit("update:modelValue", value);
}

defineExpose({
  focus: () => engineRef.value?.focus?.(),
  // ⚠️ 必须用 props.modelValue 兜底，不能写成 `?? ""`：
  // Monaco 是异步组件，chunk 还没 resolve 时 engineRef 要么是 null（占位露出来之前、失败态），
  // 要么指着加载占位（见 EditorEngineExposed 的注释），两种情况都拿不到编辑器。返回空串会让调用方
  // 以为「文档是空的」并据此覆盖掉真实内容。回落到 modelValue 才是当下真正的文档。
  getValue: () => engineRef.value?.getValue?.() ?? props.modelValue,
  setValue: (value: string) => engineRef.value?.setValue?.(value),
  // 全仓没有调用点，两份实现里也都是空的；这里只做转发，保持对外契约不变。
  format: () => engineRef.value?.format?.(),
});
</script>

<template>
  <component
    :is="engineComponent"
    ref="engineRef"
    :model-value="props.modelValue"
    :language="props.language"
    :readonly="props.readonly"
    :min-height="props.minHeight"
    :fill-height="props.fillHeight"
    :word-wrap="props.wordWrap"
    :minimap="props.minimap"
    :indent-guides="props.indentGuides"
    :whitespace="props.whitespace"
    :indent-width="props.indentWidth"
    @update:model-value="onInnerUpdate"
  />
</template>
