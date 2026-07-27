<script setup lang="ts">
import { ref } from 'vue'
import { Connection, Document, InfoFilled } from '@element-plus/icons-vue'
import type { SettingsConfigForm } from '../types'

defineProps<{
  configsSaving: boolean
  form: SettingsConfigForm
  onSave: () => void
}>()

const dockerMirrorDialogVisible = ref(false)
const binaryProxyDialogVisible = ref(false)
const proxyHelpDialogVisible = ref(false)

const dockerMirrorOptions = [
  'https://docker.1ms.run',
  'https://docker.1panel.live',
  'https://docker.sparkcr.cn',
  'https://hub.rat.dev',
  'https://dockerproxy.net',
  'https://mirror.ccs.tencentyun.com'
]

const binaryProxyOptions = [
  'https://gh-proxy.org/',
  'https://v4.gh-proxy.org/',
  'http://gh.301.ee/',
  'https://ghproxy.homeboyc.cn/'
]
</script>

<template>
  <el-card shadow="never">
    <template #header>
      <div class="card-header">
        <span class="card-title"><el-icon><Connection /></el-icon> 网络代理</span>
        <el-button type="primary" :loading="configsSaving" @click="onSave">
          <el-icon><Document /></el-icon>保存配置
        </el-button>
      </div>
    </template>

    <div class="form-field">
      <div class="field-label-row">
        <label>代理地址</label>
        <el-button
          class="field-help-button"
          text
          type="primary"
          size="small"
          @click="proxyHelpDialogVisible = true"
        >
          <el-icon><InfoFilled /></el-icon>
          说明
        </el-button>
      </div>
      <el-input v-model="form.proxy_url" placeholder="http://127.0.0.1:7890" />
      <span class="form-hint">服务器出站访问外网困难时填写；支持 HTTP/SOCKS5，如 http://127.0.0.1:7890</span>
    </div>

    <div class="form-field">
      <label>系统更新镜像源</label>
      <div class="mirror-row">
        <el-input v-model="form.update_image_mirror" placeholder="https://docker.example.com" />
        <el-button @click="dockerMirrorDialogVisible = true">
          配置
        </el-button>
        <el-button
          v-if="form.update_image_mirror"
          text
          type="danger"
          @click="form.update_image_mirror = ''"
        >
          恢复直连
        </el-button>
      </div>
      <span class="form-hint">
        Docker 部署更新使用，可填写镜像加速地址或自建镜像源；也可以到 status.anye.xyz 查看更多镜像源状态；留空则直接从默认镜像仓库拉取更新镜像。
      </span>
    </div>

    <div class="form-field">
      <label>二进制更新加速源</label>
      <div class="mirror-row">
        <el-input v-model="form.binary_update_proxy" placeholder="https://gh-proxy.example.com/" />
        <el-button @click="binaryProxyDialogVisible = true">
          配置
        </el-button>
        <el-button
          v-if="form.binary_update_proxy"
          text
          type="danger"
          @click="form.binary_update_proxy = ''"
        >
          恢复直连
        </el-button>
      </div>
      <span class="form-hint">
        二进制部署更新使用，用于加速 GitHub Release 更新包下载；留空则直连 GitHub 下载。
      </span>
    </div>

    <div class="switch-row">
      <div class="switch-item">
        <span class="switch-label">静默更新</span>
        <el-switch v-model="form.auto_update_enabled" inline-prompt active-text="开" inactive-text="关" />
      </div>
    </div>
    <span class="form-hint">开启后每 24 小时自动检查一次新版本；若检测到更新，将按当前镜像渠道自动尝试更新并通过通知渠道反馈结果。</span>

    <div class="form-field">
      <label>可信代理 CIDR</label>
      <el-input
        v-model="form.trusted_proxy_cidrs"
        type="textarea"
        :rows="5"
        placeholder="127.0.0.1/32&#10;10.0.0.0/8&#10;203.0.113.10"
      />
      <span class="form-hint">
        支持 IP、CIDR、逗号或换行分隔。留空会恢复默认私网段与本机地址；保存后客户端 IP 解析会按这份列表判断可信代理。
      </span>
    </div>

    <el-dialog v-model="proxyHelpDialogVisible" title="代理地址说明" width="560px">
      <div class="proxy-help">
        <p>
          这里配置的是面板服务器的出站代理。填写后，面板后台访问外部网络时会优先经过这个代理，例如拉取订阅仓库、下载脚本、安装 Python / Node / 系统依赖、健康检查以及部分通知请求。
        </p>
        <p>
          如果服务器本身访问 GitHub、npm、PyPI、订阅源或外部接口正常，可以留空；如果服务器在国内网络环境下经常连接超时、下载失败、依赖安装失败，或者你需要让面板通过指定代理访问外网，就填写这里。
        </p>
        <div class="proxy-help-section">
          <div class="proxy-help-title">填写示例</div>
          <code>http://127.0.0.1:7890</code>
          <code>http://user:pass@127.0.0.1:7890</code>
          <code>socks5://127.0.0.1:1080</code>
        </div>
        <p class="proxy-help-note">
          这里填写的是“面板服务器能访问到的代理地址”。如果面板运行在 Docker 容器内，127.0.0.1 指的是容器内部，不是宿主机；宿主机代理通常需要填写宿主机在容器内可访问的地址。
        </p>
      </div>
      <template #footer>
        <el-button type="primary" @click="proxyHelpDialogVisible = false">知道了</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="dockerMirrorDialogVisible" title="系统更新镜像源" width="520px">
      <div class="mirror-source-tip">
        可到
        <a href="https://status.anye.xyz/" target="_blank" rel="noopener noreferrer">
          容器镜像监控
        </a>
        查看更多 Docker Hub 镜像加速源状态，选择可用地址后手动填入上方输入框。
      </div>
      <div class="mirror-option-list">
        <button
          v-for="url in dockerMirrorOptions"
          :key="url"
          type="button"
          class="mirror-option"
          :class="{ active: form.update_image_mirror === url }"
          @click="form.update_image_mirror = url; dockerMirrorDialogVisible = false"
        >
          <span>{{ url }}</span>
        </button>
      </div>
      <template #footer>
        <el-button @click="dockerMirrorDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="binaryProxyDialogVisible" title="二进制更新加速源" width="520px">
      <div class="mirror-option-list">
        <button
          v-for="url in binaryProxyOptions"
          :key="url"
          type="button"
          class="mirror-option"
          :class="{ active: form.binary_update_proxy === url }"
          @click="form.binary_update_proxy = url; binaryProxyDialogVisible = false"
        >
          <span>{{ url }}</span>
        </button>
      </div>
      <template #footer>
        <el-button @click="binaryProxyDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<style scoped lang="scss">
@use './config-card-shared.scss' as *;

.mirror-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.field-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;

  label {
    margin-bottom: 0;
  }
}

.field-help-button {
  flex-shrink: 0;
  padding: 0 2px;
  font-size: 12px;

  .el-icon {
    margin-right: 3px;
  }
}

.proxy-help {
  display: grid;
  gap: 12px;
  color: var(--el-text-color-regular);
  font-size: 14px;
  line-height: 1.75;

  p {
    margin: 0;
  }
}

.proxy-help-section {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);

  code {
    display: block;
    padding: 7px 9px;
    border-radius: 6px;
    background: var(--el-bg-color);
    color: var(--el-text-color-primary);
    font-family: var(--dd-font-mono);
    font-size: 12px;
    word-break: break-all;
  }
}

.proxy-help-title {
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.proxy-help-note {
  color: var(--el-text-color-secondary);
}

.mirror-option-list {
  display: grid;
  gap: 8px;
}

.mirror-source-tip {
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;

  a {
    color: var(--el-color-primary);
    font-weight: 600;
    text-decoration: none;

    &:hover {
      text-decoration: underline;
    }
  }
}

.mirror-option {
  width: 100%;
  min-height: 40px;
  padding: 9px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-bg-color);
  color: var(--el-text-color-primary);
  cursor: pointer;
  text-align: left;
  font-family: var(--dd-font-mono);
  font-size: 13px;
  line-height: 1.35;
  transition: border-color 0.16s, background 0.16s, color 0.16s;

  &:hover,
  &.active {
    border-color: var(--el-color-primary);
    background: color-mix(in srgb, var(--el-color-primary) 8%, var(--el-bg-color));
    color: var(--el-color-primary);
  }
}

@media (max-width: 768px) {
  .mirror-row {
    align-items: stretch;
  }
}
</style>
