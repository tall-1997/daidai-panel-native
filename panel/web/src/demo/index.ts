import { installDemoAdapter } from './adapter'
import { installDemoBanner } from './banner'
import { DEMO_BUILD_MARKER } from './marker'

/**
 * 在线演示 Demo 的统一安装入口。
 *
 * 由 web/src/main.ts 在 `import.meta.env.VITE_DEMO === '1'` 时动态 import 调用，
 * 必须在【app.use(router)】之前执行完——不是 app.mount() 之前。
 * vue-router 的 install() 内部同步就 push 了初始地址，app.use(router) 一执行
 * router.beforeEach 就跑起来，已登录状态下刷新页面时守卫会 await fetchUser()
 * 发出 GET /auth/user；那一发请求晚一步被接管，访客就会被打回登录页。
 *
 * ⚠️ 关于产物边界（发布版必须 0 字节 demo 代码）：
 *    - 只能被 main.ts 用动态 import() 加载，任何静态 import 都会把整个 demo 层
 *      拖进真实用户的产物；
 *    - 守卫条件必须是 import.meta.env.VITE_DEMO 这个编译期常量，不能改成运行期判断
 *      （比如读 location.hostname），那样 rollup 无法做死代码消除。
 *
 * ⚠️ 不是所有 demo 代码都从这里进来。有几处调用【绕过 axios】，adapter 拦不到，
 *    只能在各自的生产文件里用同样的编译期常量守卫 + 动态 import() 分叉，
 *    因此它们各自懒加载自己那一块，与 installDemo() 无关：
 *      web/src/utils/sse.ts        -> demo/sse.ts        假实时日志流（4 个调用点共用）
 *      web/src/utils/panelSettings.ts、views/login/index.vue -> demo/shortcuts.ts
 *      web/src/api/script.ts、views/settings/useSettings*.ts -> 就地 return，不取数据
 *
 * ⚠️ 另一条容易走弯路的事实：全仓库【没有任何】 `new EventSource`，
 *    实时日志走的是 web/src/utils/sse.ts 里手写的 fetch + body.getReader()。
 *    想让假日志流跑起来，mock `window.EventSource` 是无效的，只能在 sse.ts 内部分叉。
 */
export function installDemo() {
  installDemoAdapter()

  // 横幅是直接往 body 里插 DOM 的（见 banner.ts 里为什么不写成 Vue 组件）。
  // 入口脚本是 <script type="module">（天然 defer），此刻 body 与 index.html 里的
  // #app 容器都已存在、Vue 还没往里渲染，插到 body 最前面就正好落在 #app 之上。
  installDemoBanner()

  // 这行日志同时承担两个作用：一是让访客在控制台一眼看出「后端是假的」，
  // 二是把哨兵字符串钉进产物，供 CI 的两条产物断言使用（见 demo/marker.ts）。
  console.info(`[demo] 演示环境 mock 层已安装 ${DEMO_BUILD_MARKER}`)
}
