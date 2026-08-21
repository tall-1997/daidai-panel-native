import { createApp } from "vue";
import { createPinia } from "pinia";
import "element-plus/theme-chalk/dark/css-vars.css";
import "element-plus/theme-chalk/el-loading.css";
import "element-plus/theme-chalk/el-message.css";
import "element-plus/theme-chalk/el-message-box.css";
import {
  ArrowLeft,
  ArrowRight,
  Bell,
  Box,
  Check,
  CircleCheck,
  CircleCheckFilled,
  CircleClose,
  Clock,
  Close,
  Connection,
  CopyDocument,
  Delete,
  Document,
  DocumentAdd,
  DocumentCopy,
  Download,
  Edit,
  Expand,
  Fold,
  Folder,
  FolderAdd,
  Hide,
  InfoFilled,
  Key,
  Lock,
  Menu,
  Monitor,
  Moon,
  More,
  MoreFilled,
  Odometer,
  Operation,
  Plus,
  Rank,
  Refresh,
  RefreshRight,
  Search,
  Setting,
  SetUp,
  Sort,
  Star,
  Sunny,
  Tickets,
  Timer,
  Top,
  Unlock,
  Upload,
  User,
  UserFilled,
  VideoPause,
  VideoPlay,
  View,
} from "@element-plus/icons-vue";
import App from "./App.vue";
import LoadingMotion from "./components/LoadingMotion.vue";
import router from "./router";
import { fetchAndApplyPanelAppearance } from "./utils/panelAppearance";
import "./styles/global.scss";
import "./styles/animations.css";
import "./styles/visual-enhancements.css";

// Edge / Chromium 在窗口最小化后，如果弹窗、编辑器或第三方组件延迟调用 focus()，
// 可能会把已经最小化的浏览器窗口重新拉回前台。面板后台不可见时不需要抢焦点，
// 所以统一拦截后台状态下的程序化聚焦，避免用户点击最小化后窗口又闪回。
const daidaiWindow = window as Window & {
  __DAIDAI_SAFE_FOCUS_PATCHED__?: boolean;
};

if (!daidaiWindow.__DAIDAI_SAFE_FOCUS_PATCHED__) {
  daidaiWindow.__DAIDAI_SAFE_FOCUS_PATCHED__ = true;
  const rawHTMLElementFocus = HTMLElement.prototype.focus;

  HTMLElement.prototype.focus = function safeFocus(
    this: HTMLElement,
    options?: FocusOptions,
  ) {
    if (document.visibilityState === "hidden" || !document.hasFocus()) {
      return;
    }

    rawHTMLElementFocus.call(this, options);
  };
}

async function establishLocalBrowserSession() {
  if (!window.location.pathname.startsWith("/local-ui/")) return;
  const params = new URLSearchParams(window.location.hash.slice(1));
  const ticket = params.get("ticket");
  if (!ticket) return;
  history.replaceState(null, "", window.location.pathname + window.location.search);
  const response = await fetch("/local-ui/session", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "text/plain;charset=UTF-8" },
    body: ticket,
  });
  if (!response.ok) throw new Error("本地浏览器会话已失效，请从 App 重新打开");
}

await establishLocalBrowserSession();

const app = createApp(App);

app.use(createPinia());
app.use(router);
app.component("LoadingMotion", LoadingMotion);

void fetchAndApplyPanelAppearance();

const globalIcons = {
  ArrowLeft,
  ArrowRight,
  Bell,
  Box,
  Check,
  CircleCheck,
  CircleCheckFilled,
  CircleClose,
  Clock,
  Close,
  Connection,
  CopyDocument,
  Delete,
  Document,
  DocumentAdd,
  DocumentCopy,
  Download,
  Edit,
  Expand,
  Fold,
  Folder,
  FolderAdd,
  Hide,
  InfoFilled,
  Key,
  Lock,
  Menu,
  Monitor,
  Moon,
  More,
  MoreFilled,
  Odometer,
  Operation,
  Plus,
  Rank,
  Refresh,
  RefreshRight,
  Search,
  Setting,
  SetUp,
  Sort,
  Star,
  Sunny,
  Tickets,
  Timer,
  Top,
  Unlock,
  Upload,
  User,
  UserFilled,
  VideoPause,
  VideoPlay,
  View,
};

for (const [key, component] of Object.entries(globalIcons)) {
  app.component(key, component);
}

app.mount("#app");
