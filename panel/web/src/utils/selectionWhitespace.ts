import { RangeSetBuilder } from '@codemirror/state'
import type { Extension } from '@codemirror/state'
import { Decoration, EditorView, ViewPlugin } from '@codemirror/view'
import type { DecorationSet, PluginValue, ViewUpdate } from '@codemirror/view'

/**
 * 「仅选中的空白符才显示」（issue #116-4 的 selection 档）。
 *
 * 为什么要自己写：CodeMirror 的 highlightWhitespace() **无参、只有「一直显示」这一档**，
 * 没有 Monaco renderWhitespace: 'selection' 的对应物。而 selection 恰恰是这一项的默认档
 * （也是 VS Code 的原生默认），两个引擎必须表现一致，所以补上这一层。
 *
 * ⚠️ class 名与装饰形状是**逐字复刻** @codemirror/view 的 highlightWhitespace()：
 * 空格用 `cm-highlightSpace`、Tab 用 `cm-highlightTab`，都是 Decoration.mark、按**单个字符**一段。
 * 灰点与箭头是这两个 class 的背景（CodeMirror 基础主题给底稿、
 * utils/codeEditor.ts 的 buildCodeEditorTheme() 按明暗覆盖 backgroundImage）。
 * 复刻的好处是这里一行 CSS 都不用写，也不会出现「一直显示」和「仅选中」两档灰点长得不一样。
 * 改 class 名的表现是灰点直接消失，构建与 vue-tsc 全绿。
 *
 * ⚠️ 必须是 Decoration.mark，**绝不能用 Decoration.widget**：
 * widget 会往 .cm-line 里插真实 DOM 节点（还带一对 .cm-widgetBuffer 零宽 span），
 * 那些节点会落进原生选区范围 —— 而「保住原生 Selection」正是 v3.2.0 从 Monaco 换到
 * CodeMirror 的**全部理由**（手机长按菜单 / 选择手柄 / 拖动光标）。
 * mark 只是给已有文本套一层 span，选区、复制内容都不受影响。
 * 同样的道理见 utils/indentGuides.ts 顶部。
 */

// 模块级建一次、反复复用：同一个 Decoration 实例才能让相邻字符合并进同一个 span，
// 与 highlightWhitespace() 的渲染结果完全一致。
const spaceDeco = Decoration.mark({ class: 'cm-highlightSpace' })
const tabDeco = Decoration.mark({ class: 'cm-highlightTab' })

/**
 * 单次最多画多少个空白符。
 *
 * 正常情况下工作量已经被「可见范围」卡死了（视口之外不画），这条只是兜住极端场景：
 * 全选一份缩进很深的长文件 + 屏幕很高时，一屏内的空白字符可以上万，
 * 每个都是一段装饰、一个 span，超过这个量级不如干脆少画一点也不要卡住选择手感。
 */
const MAX_MARKS = 4000

/** 空格或 Tab。与 highlightWhitespace() 内部那条 `/\t| /g` 是同一个字符集。 */
const WHITESPACE_RE = /[ \t]/g

function buildDecorations(view: EditorView): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>()
  const { doc, selection } = view.state
  let budget = MAX_MARKS

  // selection.ranges 与 view.visibleRanges 都是按位置升序且互不重叠的，
  // 两层循环取交集出来的片段自然也是升序 —— RangeSetBuilder 要求必须升序添加，乱序会直接抛错。
  for (const range of selection.ranges) {
    // 光标（空选区）什么都不画：这一档的语义就是「跟着选中的内容走」
    if (range.empty) continue

    for (const visible of view.visibleRanges) {
      const from = Math.max(range.from, visible.from)
      const to = Math.min(range.to, visible.to)
      if (from >= to) continue

      // 只取「可见范围 ∩ 选区范围」这一段文本，视口之外的选区一个字符都不扫
      const text = doc.sliceString(from, to)
      // 带 g 的正则是有状态的（lastIndex 会留在上一段的位置），换一段文本前必须清零，
      // 否则每一段都从上一段停下的下标开始扫，前面的空白会被整段漏掉
      WHITESPACE_RE.lastIndex = 0
      let match: RegExpExecArray | null
      while ((match = WHITESPACE_RE.exec(text))) {
        const at = from + match.index
        builder.add(at, at + 1, match[0] === '\t' ? tabDeco : spaceDeco)
        if (--budget <= 0) return builder.finish()
      }
    }
  }

  return builder.finish()
}

class SelectionWhitespacePlugin implements PluginValue {
  decorations: DecorationSet

  constructor(view: EditorView) {
    this.decorations = buildDecorations(view)
  }

  update(update: ViewUpdate) {
    // ⚠️ 这里必须把 selectionSet 也算进去 —— 与 indentGuides 刻意「光标移动不重算」的取向相反，
    // 但那是必要的：本插件画什么完全由选区决定，漏掉它的表现就是「选中了不出灰点、
    // 或者松开选区之后灰点赖着不走」。
    // docChanged / viewportChanged 同样不能少：改文档会让位置整体位移，滚动会换可见范围。
    if (update.selectionSet || update.docChanged || update.viewportChanged) {
      this.decorations = buildDecorations(update.view)
    }
  }
}

export function selectionWhitespace(): Extension {
  return ViewPlugin.fromClass(SelectionWhitespacePlugin, {
    decorations: (plugin) => plugin.decorations
  })
}
