<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { EditorState } from "@codemirror/state";
import { EditorView, highlightSpecialChars, lineNumbers } from "@codemirror/view";
import {
  Change,
  MergeView,
  diff,
  getChunks,
  unifiedMergeView,
} from "@codemirror/merge";
import {
  buildCodeEditorTheme,
  isCodeEditorDark,
  resolveCodeEditorLanguage,
} from "@/utils/codeEditor";
import { PANEL_APPEARANCE_CHANGE_EVENT } from "@/utils/panelAppearance";

/**
 * 版本对比编辑器的 **CodeMirror 6 实现**（@codemirror/merge）。
 *
 * 它不直接被页面使用：调用点用的是 CodeDiffEditor.vue，那是个按引擎偏好选实现的分发层，
 * 本组件与 MonacoDiffEditor.vue 是它的两个可选后端。
 *
 * ⚠️ 对外 props（名字、类型、默认值）必须与 MonacoDiffEditor.vue **逐字一致**：
 * 分发层是逐个显式往下传的，两边对不上不会报错，只会在切到某个引擎时静默丢掉一个开关。
 *
 * CodeMirror 是默认引擎、也是移动端唯一引擎（理由见 utils/editorEngine.ts 顶部）。
 */
const props = withDefaults(
  defineProps<{
    originalValue: string;
    modifiedValue: string;
    language?: string;
    readonly?: boolean;
    renderSideBySide?: boolean;
    ignoreTrimWhitespace?: boolean;
    hideUnchangedRegions?: boolean;
    contextLineCount?: number;
  }>(),
  {
    language: "plaintext",
    readonly: true,
    renderSideBySide: true,
    ignoreTrimWhitespace: false,
    hideUnchangedRegions: false,
    contextLineCount: 3,
  },
);

/** 行首尾要抹掉的空白。\r 也算：顺带让 CRLF 与 LF 两种换行不被判成差异。 */
function isEdgeWhitespace(char: string | undefined) {
  return char === " " || char === "\t" || char === "\r";
}

/**
 * 抹掉每一行**首尾**的空白，同时给出「抹掉后的下标 → 原文下标」的映射表。
 * 映射表长度是抹掉后文本长度 + 1：Change 的 to 是开区间右端，可能正好等于文本长度。
 *
 * 为什么是 trim（首尾都抹）而不是只抹行尾：这是在对齐 Monaco 的 ignoreTrimWhitespace 语义 ——
 * 它按 trim 比较，而缩进正是行首的空白。开关文案是「忽略空白差异」、说明写的是
 * 「只检测到空格、缩进或换行变化」，所以「一处真实改动 + 一段重新缩进」的版本对比里，
 * 只改了缩进的那段在开着开关时必须不算差异；只抹行尾会让它重新被标红，
 * 属于换引擎带来的、没在发布说明里声明过的行为回退。
 */
function stripLineEdgeWhitespace(text: string) {
  let stripped = "";
  const map: number[] = [];
  let lineStart = 0;

  for (;;) {
    const newlineAt = text.indexOf("\n", lineStart);
    const lineEnd = newlineAt === -1 ? text.length : newlineAt;
    let contentEnd = lineEnd;
    while (contentEnd > lineStart && isEdgeWhitespace(text[contentEnd - 1])) {
      contentEnd -= 1;
    }
    // 行首必须在 contentEnd 算完之后再往前收，且以 contentEnd 为界：
    // 整行都是空白时两个游标会停在同一处，slice 出空串、map 也不会塞进倒序下标，
    // 从而保住「map 单调递增、长度 = stripped.length + 1」这两条不变量。
    let contentStart = lineStart;
    while (contentStart < contentEnd && isEdgeWhitespace(text[contentStart])) {
      contentStart += 1;
    }
    // map 只记「被保留下来的字符」的原文下标，行首被抹掉的那几个下标直接跳过
    for (let i = contentStart; i < contentEnd; i += 1) {
      map.push(i);
    }
    stripped += text.slice(contentStart, contentEnd);
    if (newlineAt === -1) {
      break;
    }
    map.push(newlineAt);
    stripped += "\n";
    lineStart = newlineAt + 1;
  }

  map.push(text.length);
  return { text: stripped, map };
}

/**
 * 「忽略空白差异」的自定义 diff。
 *
 * CodeMirror 的 diff 没有 Monaco 那个内置的 ignoreTrimWhitespace 开关，
 * 只能通过 diffConfig.override 整个接管：先把两侧每行首尾的空白抹掉再比，
 * 再把结果下标映射回原文——这样显示出来的仍然是原文（不动用户的内容），
 * 但纯粹的缩进 / 行尾空白改动不会被算成差异。
 *
 * 这里刻意用 diff 而不是 presentableDiff：override 的返回值会被调用方
 * （Chunk.build）再跑一遍 makePresentable，先跑一次纯属重复劳动。
 */
function diffIgnoringLineEdgeWhitespace(a: string, b: string) {
  const strippedA = stripLineEdgeWhitespace(a);
  const strippedB = stripLineEdgeWhitespace(b);
  return diff(strippedA.text, strippedB.text, { scanLimit: 500 }).map(
    (change) =>
      new Change(
        strippedA.map[change.fromA]!,
        strippedA.map[change.toA]!,
        strippedB.map[change.fromB]!,
        strippedB.map[change.toB]!,
      ),
  );
}

const containerRef = ref<HTMLElement>();
let mergeView: MergeView | null = null;
let unifiedView: EditorView | null = null;

/**
 * 差异总览标尺（issue #114-4）。
 *
 * 就是 Monaco 那条「滚动条上的红绿标记」：一眼看出整份 diff 的改动分布，点一下跳过去。
 * CodeMirror 自带的只有行号旁边那条 3px 的 .cm-changeGutter，它是**随内容滚动**的，
 * 只能告诉你当前屏幕上这几行有没有改，答不了「一共改了几处、都在哪」。
 *
 * ⚠️ 这套东西是 CodeMirror 侧**独有**的，别往 MonacoDiffEditor.vue 移植：
 * Monaco 的 renderOverviewRuler 原生就画这条标尺，移植过去是两条标尺打架。
 *
 * ⚠️ 刻意做成编辑器 DOM **之外**的一层覆盖物，而不是 CM6 的 ViewPlugin：
 *   1. 并排模式下两个 .cm-editor 被 merge 的基础主题按成了 height:auto / overflow:visible，
 *      编辑器内部根本没有一个定高的盒子可以让标尺贴边；真正带滚动条的是外层 .cm-mergeView。
 *   2. shared 里的扩展会同时进 A、B 两侧，做成插件会画出两条标尺，
 *      而「我是哪一侧」要读 merge 的私有 facet，拿不到。
 *   3. 最关键：ViewPlugin 想接点击就得挂 EditorView.domEventHandlers，
 *      那正好是本编辑器**绝不能碰**的东西 —— 内容区靠原生 Selection 撑着手机上的
 *      长按菜单 / 选择手柄 / 拖光标。标尺是 .code-diff-container 的兄弟节点，
 *      只在自己身上挂 click，与内容区零交集。后人别把它「优化」成插件。
 */
type DiffTick = {
  /** 跳转用的文档位置（B 侧） */
  pos: number;
  /** 占滚动内容总高的百分比 */
  top: number;
  height: number;
  kind: "insert" | "delete" | "modify";
};

const ticks = ref<DiffTick[]>([]);
const viewportTop = ref(0);
const viewportHeight = ref(0);
const rulerDark = ref(true);
/** 真正带滚动条的那个元素：并排是 .cm-mergeView，统一模式是 .cm-scroller */
let scrollEl: HTMLElement | null = null;
let rulerFrame = 0;

/**
 * 重新量一遍标尺。
 *
 * 一律以 B 侧（新版本）为基准：并排模式下 merge 会用占位块把两侧逐块对齐，
 * 所以 B 的行坐标就是整个滚动容器的行坐标，连「左边删掉的那几行」也已经被占位块撑出了高度；
 * 统一模式本来就只有 B 一侧。
 *
 * ⚠️ 这里只读不写，**绝不能在里面 dispatch** —— 它是被 updateListener 调起来的，
 * 一 dispatch 就是死循环。
 */
function refreshRuler() {
  const view = mergeView ? mergeView.b : unifiedView;
  const chunks = view ? getChunks(view.state)?.chunks : null;
  const scrollHeight = scrollEl?.scrollHeight ?? 0;
  if (!view || !scrollEl || !chunks?.length || scrollHeight <= 0) {
    ticks.value = [];
    return;
  }

  // lineBlockAt().top 是相对「文档第一行顶部」的，先换算成「滚动内容顶部」的偏移，
  // 这样才和 scrollTop / scrollHeight 处在同一套坐标里。
  const docTop =
    view.documentTop - scrollEl.getBoundingClientRect().top + scrollEl.scrollTop;
  const docLength = view.state.doc.length;

  ticks.value = chunks.map((chunk) => {
    // toB 可能指到文档末尾之后（@codemirror/merge 的文档里写明了这一点），
    // endB 是它自己给出的合法版本；再夹一次文档长度兜底。
    const from = Math.min(chunk.fromB, docLength);
    const to = Math.min(Math.max(chunk.endB, from), docLength);
    const top = docTop + view.lineBlockAt(from).top;
    const bottom = docTop + view.lineBlockAt(to).bottom;
    return {
      pos: from,
      top: (top / scrollHeight) * 100,
      height: ((bottom - top) / scrollHeight) * 100,
      // 分类口径与 merge 自己画装饰时一致：某一侧为空 = 纯增 / 纯删
      kind:
        chunk.fromB === chunk.toB
          ? "delete"
          : chunk.fromA === chunk.toA
            ? "insert"
            : "modify",
    } satisfies DiffTick;
  });

  viewportTop.value = (scrollEl.scrollTop / scrollHeight) * 100;
  viewportHeight.value = (scrollEl.clientHeight / scrollHeight) * 100;
}

/**
 * 把同一帧里的多次刷新并成一次。
 * 并排模式下 updateListener 装在 shared 里、A 和 B 各会报一次，
 * merge 自己的占位块测量结束后还会再触发一轮，不合并就是一帧量四遍。
 */
function scheduleRulerRefresh() {
  if (rulerFrame) return;
  rulerFrame = requestAnimationFrame(() => {
    rulerFrame = 0;
    refreshRuler();
  });
}

/**
 * 点某一条刻度：滚到那个差异块。
 * 用 scrollIntoView 而不是直接改 scrollTop —— 视口外的行高只是估算值，
 * 交给 CodeMirror 自己边渲染边修正更准。
 */
function jumpToTick(tick: DiffTick) {
  const view = mergeView ? mergeView.b : unifiedView;
  view?.dispatch({
    effects: EditorView.scrollIntoView(tick.pos, { y: "center" }),
  });
}

/** 点标尺空白处：按比例跳，等价于点滚动条轨道 */
function jumpToRulerPoint(event: MouseEvent) {
  if (!scrollEl) return;
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
  const fraction = (event.clientY - rect.top) / rect.height;
  const max = Math.max(scrollEl.scrollHeight - scrollEl.clientHeight, 0);
  const next = fraction * scrollEl.scrollHeight - scrollEl.clientHeight / 2;
  scrollEl.scrollTop = Math.min(Math.max(next, 0), max);
}

function destroyView() {
  // 标尺的三样东西必须在拆编辑器之前清干净：
  // 留着监听器会吊住一个已经销毁的 .cm-mergeView，切到另一个历史版本时
  // 还会有一帧显示上一对版本的刻度。
  if (rulerFrame) {
    cancelAnimationFrame(rulerFrame);
    rulerFrame = 0;
  }
  scrollEl?.removeEventListener("scroll", scheduleRulerRefresh);
  scrollEl = null;
  ticks.value = [];

  mergeView?.destroy();
  mergeView = null;
  unifiedView?.destroy();
  unifiedView = null;
  // destroy() 只负责拆编辑器自身，外层 DOM 是我们传 parent 时挂上去的，
  // 不清干净的话重建时会在同一个容器里叠出两份。
  if (containerRef.value) {
    containerRef.value.innerHTML = "";
  }
}

/**
 * 整体重建。
 *
 * 这里刻意不做「按 prop 逐项 reconfigure」：本组件是只读的一次性对比视图，
 * 每次 prop 变化基本都意味着换了一对要比的内容（选了另一个历史版本、切了移动端布局），
 * 重建比维护 6 个 Compartment 简单得多，代价只是丢掉滚动位置。
 */
function buildView() {
  destroyView();
  const container = containerRef.value;
  if (!container) return;

  const diffConfig = props.ignoreTrimWhitespace
    ? { scanLimit: 500, override: diffIgnoringLineEdgeWhitespace }
    : { scanLimit: 500 };
  // margin 对应 Monaco 的 contextLineCount（差异块上下各保留几行上下文）；
  // minSize 是 CodeMirror 独有的「至少这么多行才值得折叠」，取它的默认值 4。
  const collapseUnchanged = props.hideUnchangedRegions
    ? { margin: props.contextLineCount, minSize: 4 }
    : undefined;

  const shared = [
    lineNumbers(),
    highlightSpecialChars(),
    // 改造前 Monaco 侧是 wordWrap/diffWordWrap 都为 'on'，这里保持一致
    EditorView.lineWrapping,
    resolveCodeEditorLanguage(props.language),
    // geometryChanged 覆盖了：初次测量完成、两侧占位块对齐完成、
    // 折叠段被用户展开、容器尺寸变化。少了它标尺会停在第一帧的估算值上。
    EditorView.updateListener.of((update) => {
      if (update.geometryChanged || update.docChanged) scheduleRulerRefresh();
    }),
    ...buildCodeEditorTheme(),
  ];

  if (props.renderSideBySide) {
    mergeView = new MergeView({
      parent: container,
      a: {
        doc: props.originalValue,
        // 左侧是历史版本，任何时候都不可编辑（改造前 Monaco 侧是 originalEditable: false）
        extensions: [
          ...shared,
          EditorState.readOnly.of(true),
          EditorView.editable.of(false),
        ],
      },
      b: {
        doc: props.modifiedValue,
        extensions: [
          ...shared,
          EditorState.readOnly.of(props.readonly),
          EditorView.editable.of(!props.readonly),
        ],
      },
      gutter: true,
      highlightChanges: true,
      diffConfig,
      collapseUnchanged,
    });
  } else {
    // renderSideBySide=false（移动端）走统一视图：只有一个编辑器，
    // 删除的内容以只读小部件的形式插在新内容上方。
    unifiedView = new EditorView({
      parent: container,
      doc: props.modifiedValue,
      extensions: [
        ...shared,
        EditorState.readOnly.of(props.readonly),
        EditorView.editable.of(!props.readonly),
        unifiedMergeView({
          original: props.originalValue,
          // 只读的版本对比，不需要逐块「接受 / 拒绝」按钮
          mergeControls: false,
          gutter: true,
          highlightChanges: true,
          diffConfig,
          collapseUnchanged,
        }),
      ],
    });
  }

  // 标尺初始化，两种模式共用。
  // 并排模式的滚动条在外层 .cm-mergeView 上（它自带 overflow-y: auto，
  // 里面两个 .cm-scroller 被 merge 的基础主题按成了 overflow: visible）；
  // 统一模式没有这层外壳，就是普通编辑器的 .cm-scroller。
  rulerDark.value = isCodeEditorDark();
  scrollEl = mergeView ? mergeView.dom : unifiedView!.scrollDOM;
  scrollEl.addEventListener("scroll", scheduleRulerRefresh, { passive: true });
  scheduleRulerRefresh();
}

onMounted(() => {
  // 明暗切换与「自定义编辑器底色」都走这个事件（见 stores/theme.ts 与 utils/panelAppearance.ts）。
  // 这里直接重建，理由同 buildView 的注释：代价只有滚动位置，而改主题时本来也没在滚。
  window.addEventListener(PANEL_APPEARANCE_CHANGE_EVENT, buildView);
  buildView();
});

watch(
  [
    () => props.originalValue,
    () => props.modifiedValue,
    () => props.language,
    () => props.readonly,
    () => props.renderSideBySide,
    () => props.ignoreTrimWhitespace,
    () => props.hideUnchangedRegions,
    () => props.contextLineCount,
  ],
  () => {
    buildView();
  },
);

onBeforeUnmount(() => {
  window.removeEventListener(PANEL_APPEARANCE_CHANGE_EVENT, buildView);
  destroyView();
});
</script>

<template>
  <div class="code-diff-wrapper">
    <div ref="containerRef" class="code-diff-container"></div>
    <!-- 差异总览标尺。刻意放在编辑器 DOM 之外，理由见 script 里 DiffTick 上方那段注释：
         编辑器没启用 drawSelection、靠的是原生选区，任何挂到内容区上的指针处理
         都会毁掉手机上的长按菜单和选择手柄。 -->
    <div
      v-if="ticks.length"
      class="code-diff-ruler"
      :class="{ 'is-dark': rulerDark }"
      role="presentation"
      @click="jumpToRulerPoint"
    >
      <div
        class="code-diff-ruler__viewport"
        :style="{ top: `${viewportTop}%`, height: `${viewportHeight}%` }"
      ></div>
      <button
        v-for="(tick, index) in ticks"
        :key="index"
        type="button"
        class="code-diff-ruler__tick"
        :class="`is-${tick.kind}`"
        :aria-label="`跳到第 ${index + 1} 处差异`"
        :style="{ top: `${tick.top}%`, height: `${tick.height}%` }"
        @click.stop="jumpToTick(tick)"
      ></button>
    </div>
  </div>
</template>

<style scoped>
.code-diff-wrapper {
  width: 100%;
  height: 100%;
  min-height: 420px;
  position: relative;
  overflow: hidden;
  font-size: 14px;
  background: var(--dd-editor-bg-color, #111827);
}

/* 给右侧标尺让出 10px，而不是把标尺盖在原生滚动条上面：
   盖上去会挡住滚动条拖动；而「往左偏一个滚动条宽度」在 macOS / 手机上不成立
   —— 那里的浮层滚动条宽度是 0，偏移量会算错。
   ⚠️ 这 10px 是标尺的专属槽位，MonacoDiffEditor.vue 那边没有标尺，宽度必须是 100%。 */
.code-diff-container {
  width: calc(100% - 10px);
  height: 100%;
}

.code-diff-ruler {
  position: absolute;
  inset-block: 0;
  inset-inline-end: 0;
  width: 10px;
  cursor: pointer;
  /* 自带一层中性底：编辑器底色是用户可配的（可以调成红的或绿的），
     不铺底的话刻度会在同色底上直接消失。 */
  background: rgba(127, 127, 127, 0.12);
}

.code-diff-ruler__viewport {
  position: absolute;
  inset-inline: 0;
  background: rgba(127, 127, 127, 0.22);
  pointer-events: none;
}

.code-diff-ruler__tick {
  position: absolute;
  inset-inline: 1px;
  /* 只改了一行的块换算成百分比可能不到 1px，兜一个最小高度免得看不见 */
  min-height: 3px;
  padding: 0;
  border: none;
  /* 8px 宽的分类色标：≤12px 的装饰色标固定 2px、不吃 --dd-radius-* 三档刻度
     （吃 control 档在 rounded 模式下会被收缩规则夹成圆点，和状态灯撞脸）。 */
  border-radius: 2px;
  cursor: pointer;
}

/* 红绿取自 @codemirror/merge 基础主题里 .cm-changedLineGutter / .cm-deletedLineGutter
   的原值，保证「标尺上的红绿」和「行号旁边那条 3px 红绿」严格同色 ——
   这属于语义识别色，是硬规则 1 允许的例外。
   明暗**不能**用 prefers-color-scheme：这里的明暗取决于用户自配的编辑器底色，
   由 isCodeEditorDark() 判定后打在 .is-dark 上。 */
.code-diff-ruler__tick.is-delete {
  background: #e43;
}

.code-diff-ruler__tick.is-insert {
  background: #2b2;
}

.code-diff-ruler__tick.is-modify {
  background: linear-gradient(90deg, #e43 50%, #2b2 50%);
}

.code-diff-ruler.is-dark .code-diff-ruler__tick.is-delete {
  background: #fa9;
}

.code-diff-ruler.is-dark .code-diff-ruler__tick.is-insert {
  background: #8f8;
}

.code-diff-ruler.is-dark .code-diff-ruler__tick.is-modify {
  background: linear-gradient(90deg, #fa9 50%, #8f8 50%);
}

/* 并排模式：滚动条在 .cm-mergeView 上（它自带 overflow-y: auto），
   内部两个 .cm-editor 被 merge 的基础主题强制成 height:auto !important，不用也不能再管。 */
.code-diff-container :deep(.cm-mergeView) {
  height: 100%;
}

/* 统一模式没有 .cm-mergeView 外壳，就是一个普通编辑器，得自己撑满 */
.code-diff-container :deep(.cm-editor) {
  height: 100%;
}

/* iOS Safari 在可编辑区字号小于 16px 时聚焦会放大整页。
   对比视图默认只读、一般不会聚焦，但移动端走的是统一视图、内容区仍是 contenteditable，
   与 CodeEditor.vue 保持同一套规避方式。 */
@media (max-width: 768px) {
  .code-diff-wrapper {
    font-size: 16px;
  }
}
</style>
