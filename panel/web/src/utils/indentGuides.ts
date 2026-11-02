import { getIndentUnit } from '@codemirror/language'
import { countColumn, RangeSetBuilder } from '@codemirror/state'
import type { EditorState, Extension } from '@codemirror/state'
import { Decoration, EditorView, ViewPlugin } from '@codemirror/view'
import type { DecorationSet, PluginValue, ViewUpdate } from '@codemirror/view'

/**
 * 缩进参考线（每一级缩进画一条竖线，issue #114-2）。
 *
 * ⚠️ 实现方式是**唯一**能选的那种，改之前先读这段。
 * 本编辑器刻意没有启用 drawSelection —— 内容区是 contenteditable、选区就是原生 Selection，
 * 手机上的长按菜单 / 选择手柄 / 拖动光标全靠它（见 CodeMirrorEditor.vue 顶部注释）。
 * 所以参考线**不能**是 Decoration.widget：widget 会在 .cm-line 里塞进真实 DOM 节点
 * （还会顺带插一对 .cm-widgetBuffer 零宽 span），那些节点会落进原生选区范围，
 * 影响长按选择的落点、也可能被系统「拷贝」抄进剪贴板。
 *
 * 这里用的是 Decoration.line：它只往**已经存在**的 .cm-line 上加 class 和 style，
 * 一个新节点都不建，线是用 CSS 背景渐变画的 —— 背景不是内容，进不了选区、也拷不走。
 */

const GUIDE_CLASS = 'cm-dd-indent-guide'
/** 线的颜色。由 buildCodeEditorTheme() 按明暗算好后写进主题，这里只引用。 */
const GUIDE_COLOR_VAR = '--dd-indent-guide-color'
/** 相邻两条线的间距（= 一级缩进的字符宽） */
const GUIDE_STEP_VAR = '--dd-indent-guide-step'
/** 背景绘制区的宽度，用来卡住「这一行只画 level 条线」 */
const GUIDE_WIDTH_VAR = '--dd-indent-guide-width'

/**
 * 空行往上/往下找非空行时的最大扫描行数。
 * 不设上限的话，一份「开头几千个空行」的文件每次滚动都要从头扫一遍。
 */
const BLANK_SCAN_LIMIT = 500

/** 一行开头的空白占多少个显示列（tab 按 tabSize 补齐，不是按 1 个字符算） */
function leadingColumns(text: string, tabSize: number) {
  let i = 0
  while (i < text.length) {
    const ch = text[i]
    if (ch !== ' ' && ch !== '\t' && ch !== '　') break
    i++
  }
  return countColumn(text, tabSize, i)
}

/**
 * 行号 → 缩进级别。带 memo，因为空行要反复向前后探邻居。
 */
class IndentLevels {
  private cache = new Map<number, { level: number; blank: boolean }>()
  private state: EditorState
  private unit: number
  private tabSize: number

  // 这里必须逐条手写赋值，不能用 TS 的构造函数参数属性简写：
  // 本仓 tsconfig 开了 erasableSyntaxOnly，`constructor(private x: T)` 会直接编译报错。
  constructor(state: EditorState, unit: number, tabSize: number) {
    this.state = state
    this.unit = unit
    this.tabSize = tabSize
  }

  private raw(lineNo: number) {
    const cached = this.cache.get(lineNo)
    if (cached) return cached
    const text = this.state.doc.line(lineNo).text
    const blank = !/\S/.test(text)
    const entry = {
      blank,
      level: blank ? 0 : Math.floor(leadingColumns(text, this.tabSize) / this.unit)
    }
    this.cache.set(lineNo, entry)
    return entry
  }

  private nearestCodeLevel(from: number, dir: -1 | 1) {
    const total = this.state.doc.lines
    let n = from + dir
    for (let steps = 0; steps < BLANK_SCAN_LIMIT && n >= 1 && n <= total; steps++, n += dir) {
      const entry = this.raw(n)
      if (!entry.blank) return entry.level
    }
    return 0
  }

  levelOf(lineNo: number) {
    const entry = this.raw(lineNo)
    if (!entry.blank) return entry.level

    // 空行必须接上参考线，否则块中间空一行、线就断一截，很丑。
    const prev = this.nearestCodeLevel(lineNo, -1)
    const next = this.nearestCodeLevel(lineNo, 1)
    // 上一段不比下一段浅 —— 说明这个空行还在上一段那个块里面，跟随上一段。
    if (prev >= next) return prev
    // 剩下的情况是「空行之后才开始变深」，也就是空行卡在块的开头。
    // 只跟到深一级：直接用 next 的话，一个 4 级深的块会让它上面那条孤零零的空行
    // 凭空长出 4 条线，看着像是空行自己有缩进。
    return prev + 1
  }
}

function buildGuides(view: EditorView): DecorationSet {
  const { state } = view
  const unit = getIndentUnit(state)
  const builder = new RangeSetBuilder<Decoration>()
  if (unit <= 0) return builder.finish()

  const levels = new IndentLevels(state, unit, state.tabSize)
  // visibleRanges 之间可能被折叠切开，同一行有机会出现两次，
  // 同一位置加两次会让 RangeSetBuilder 直接抛错
  let lastLine = 0

  for (const { from, to } of view.visibleRanges) {
    let pos = from
    while (pos <= to) {
      const line = state.doc.lineAt(pos)
      pos = line.to + 1
      if (line.number <= lastLine) continue
      lastLine = line.number

      const level = levels.levelOf(line.number)
      if (level <= 0) continue

      builder.add(
        line.from,
        line.from,
        Decoration.line({
          class: GUIDE_CLASS,
          attributes: {
            // 只写自定义属性，画线的那几条 background-* 长写法在下面的 baseTheme 里，
            // 免得每一行都重复一遍一模一样的渐变字符串。
            style:
              `${GUIDE_STEP_VAR}: ${unit}ch;` +
              // 减 1px：背景框正好卡在第 level 条线的位置上，亚像素取整时
              // 右边缘会漏出半条多余的线，砍掉 1px 就不会。
              `${GUIDE_WIDTH_VAR}: calc(${level * unit}ch - 1px)`
          }
        })
      )
    }
  }

  return builder.finish()
}

class IndentGuidePlugin implements PluginValue {
  decorations: DecorationSet
  private unit: number
  private tabSize: number

  constructor(view: EditorView) {
    this.unit = getIndentUnit(view.state)
    this.tabSize = view.state.tabSize
    this.decorations = buildGuides(view)
  }

  update(update: ViewUpdate) {
    const unit = getIndentUnit(update.state)
    const tabSize = update.state.tabSize
    // ⚠️ tabSize 必须和 unit 一起纳入判据：buildGuides 里 leadingColumns() 是按 tabSize
    // 把 \t 补齐成列数的，只盯 unit 的话，「缩进宽度」改了而 unit 恰好没变的那一路
    // （比如文档换了、自动检测出同一个 unit 但 tabSize 被重配）参考线会停在旧格子上。
    // 本项目里这两条永远是一起 reconfigure 的（见 CodeMirrorEditor.vue 的 buildIndentExtension），
    // 但少一条判据就是一个等着被踩的静默失效。
    const metricsChanged = unit !== this.unit || tabSize !== this.tabSize
    this.unit = unit
    this.tabSize = tabSize
    // 只在这三件事上重算：改文档、滚动换视口、缩进度量被 reconfigure。
    // **光标移动不重算** —— 手机上每敲一下都全量重算会明显掉帧。
    if (update.docChanged || update.viewportChanged || metricsChanged) {
      this.decorations = buildGuides(update.view)
    }
  }
}

/**
 * 参考线的静态样式。
 *
 * ⚠️ 全部用 background-* 长写法，**不要**图省事写成 `background:` 简写：
 * 简写会把 background-color 一并重置掉，当前行高亮（.cm-activeLine 只设了
 * background-color）会当场消失，而且只在光标所在那一行复现，极容易漏测。
 *
 * background-origin: content-box 是对齐的关键：.cm-line 有 `padding: 0 2px 0 6px`，
 * 背景定位区从**内容盒**起算才正好是代码第一个字符的位置。写死 `left: 6px` 也能对上，
 * 但 CodeMirror 改过这个 padding（4px → 6px），写死等于埋一颗跟着上游漂移的雷。
 */
const guideBaseTheme = EditorView.baseTheme({
  [`.${GUIDE_CLASS}`]: {
    backgroundImage: `repeating-linear-gradient(to right, var(${GUIDE_COLOR_VAR}, rgba(127, 127, 127, 0.28)) 0 1px, transparent 1px var(${GUIDE_STEP_VAR}, 2ch))`,
    backgroundRepeat: 'no-repeat',
    backgroundOrigin: 'content-box',
    backgroundPosition: '0 0',
    // 纵向 100%：一行被自动换行折成好几行时，.cm-line 这个块盒本来就包住了全部折行，
    // 背景跟着一起长，参考线自然穿过所有续行 —— 换行模式不用额外处理。
    backgroundSize: `var(${GUIDE_WIDTH_VAR}, 0) 100%`
  }
})

export function indentGuides(): Extension {
  return [
    guideBaseTheme,
    ViewPlugin.fromClass(IndentGuidePlugin, {
      decorations: (plugin) => plugin.decorations
    })
  ]
}
