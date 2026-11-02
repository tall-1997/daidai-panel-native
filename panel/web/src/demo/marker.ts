/**
 * 产物门禁哨兵（`.github/workflows/checks.yml` 的两条断言就是靠它判定的）。
 *
 * 为什么必须是一个【字符串字面量】，而不能直接 grep `installDemo` 这类函数名：
 *   Vite 的应用构建开着 esbuild `minifyIdentifiers`，rollup 又对 `es` 格式默认开启
 *   `minifyInternalExports`，所以导出名在产物里会被压成 `a` 之类的单字母。
 *   拿函数名去 grep，发布版和 Demo 版都是 0 命中——门禁看着是绿的，实际上什么都没守。
 *   模块路径（如 `demo/fixtures`）同样不会出现在产物里。字符串字面量不会被改名，
 *   是唯一可靠的判据。
 *
 * ⚠️ 单独抽成一个模块，是因为 demo 层现在有【三个】入口，各自被不同的生产文件
 *    用动态 import() 拉起来：
 *      demo/index.ts     <- main.ts（adapter + 横幅）
 *      demo/sse.ts       <- utils/sse.ts（假实时日志流）
 *      demo/shortcuts.ts <- utils/panelSettings.ts、views/login/index.vue
 *    只在 index.ts 里放哨兵是不够的：万一哪天有人把 shortcuts 改成静态 import，
 *    泄漏出去的只有 shortcuts 那一支，index.ts 仍然被剔除，
 *    「发布版不含 mock 代码」那条断言会照样绿灯放行。
 *    ⇒ 每个入口都要在自己的【活代码】里引用一次这个常量。
 *      光 import 不引用是没用的，会被 tree-shaking 掉。
 */
export const DEMO_BUILD_MARKER = '__DAIDAI_DEMO_MOCK__'
