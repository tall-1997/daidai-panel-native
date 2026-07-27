<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { SponsorRecord, SponsorSummary } from '@/api/sponsor'

const props = defineProps<{
  sponsors: SponsorRecord[]
  summary: SponsorSummary | null
  loading: boolean
}>()

const avatarRetryDelayMs = 1800
const avatarRetryLimit = 2
const brokenAvatarKeys = ref<string[]>([])
const avatarRetryAttempts = ref<Record<string, number>>({})
const avatarRetryTimers = new Map<string, ReturnType<typeof setTimeout>>()

function sponsorAvatarKey(sponsor: Pick<SponsorRecord, 'id' | 'avatar_url'>) {
  return `${sponsor.id}:${sponsor.avatar_url || ''}`
}

function clearAvatarRetryTimer(key: string) {
  const timer = avatarRetryTimers.get(key)
  if (timer) {
    clearTimeout(timer)
    avatarRetryTimers.delete(key)
  }
}

watch(
  () => props.sponsors.map(sponsorAvatarKey),
  (keys) => {
    const activeKeys = new Set(keys)
    brokenAvatarKeys.value = brokenAvatarKeys.value.filter((key) => activeKeys.has(key))
    avatarRetryAttempts.value = Object.fromEntries(
      Object.entries(avatarRetryAttempts.value).filter(([key]) => activeKeys.has(key))
    )

    for (const key of Array.from(avatarRetryTimers.keys())) {
      if (!activeKeys.has(key)) {
        clearAvatarRetryTimer(key)
      }
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  for (const key of Array.from(avatarRetryTimers.keys())) {
    clearAvatarRetryTimer(key)
  }
})

const sortedSponsors = computed(() => {
  return [...props.sponsors].sort((left, right) => {
    const amountDiff = (Number(right.amount) || 0) - (Number(left.amount) || 0)
    if (amountDiff !== 0) return amountDiff

    const rightTime = right.updated_at ? new Date(right.updated_at).getTime() : 0
    const leftTime = left.updated_at ? new Date(left.updated_at).getTime() : 0
    return rightTime - leftTime
  })
})

type PodiumSlot = 'first' | 'second' | 'third'

interface PodiumEntry {
  slot: PodiumSlot
  rank: 1 | 2 | 3
  sponsor: SponsorRecord | null
}

const podiumSponsors = computed<PodiumEntry[]>(() => {
  const [first, second, third] = sortedSponsors.value
  return [
    { slot: 'first', rank: 1, sponsor: first || null },
    { slot: 'second', rank: 2, sponsor: second || null },
    { slot: 'third', rank: 3, sponsor: third || null },
  ]
})

const remainingSponsors = computed(() => sortedSponsors.value.slice(3))

const sponsorServiceUnavailable = computed(() => !!props.summary?.unavailable)

function markAvatarBroken(sponsor: SponsorRecord) {
  const key = sponsorAvatarKey(sponsor)
  const attempt = (avatarRetryAttempts.value[key] || 0) + 1
  avatarRetryAttempts.value = {
    ...avatarRetryAttempts.value,
    [key]: attempt,
  }

  if (!brokenAvatarKeys.value.includes(key)) {
    brokenAvatarKeys.value = [...brokenAvatarKeys.value, key]
  }

  if (attempt > avatarRetryLimit) {
    clearAvatarRetryTimer(key)
    return
  }

  clearAvatarRetryTimer(key)
  avatarRetryTimers.set(key, setTimeout(() => {
    clearAvatarRetryTimer(key)
    brokenAvatarKeys.value = brokenAvatarKeys.value.filter((item) => item !== key)
  }, avatarRetryDelayMs * attempt))
}

function isAvatarBroken(sponsor: SponsorRecord) {
  return brokenAvatarKeys.value.includes(sponsorAvatarKey(sponsor))
}

function formatAmount(amount: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount || 0)
}

function rankLabel(rank: number) {
  if (rank === 1) return '第一名'
  if (rank === 2) return '第二名'
  return '第三名'
}
</script>

<template>
  <el-card shadow="hover" class="sponsor-wall">
    <template #header>
      <div class="sponsor-wall__header">
        <div class="card-title-bar sponsor-wall__header-copy">
          <span class="title-dot" style="background: linear-gradient(180deg, #f97316, #facc15)"></span>
          <span class="title-text">赞助名单</span>
        </div>
        <div class="sponsor-wall__summary" v-if="summary">
          <span class="summary-pill">{{ summary.count }} 位支持者</span>
          <span class="summary-pill">{{ formatAmount(summary.total_amount) }}</span>
        </div>
      </div>
    </template>

    <div v-if="loading && sponsors.length === 0" class="sponsor-loading">
      <span v-for="item in 6" :key="item" class="sponsor-loading__card"></span>
    </div>

    <div v-else-if="sponsors.length === 0" class="sponsor-empty">
      <h4>{{ sponsorServiceUnavailable ? '赞助名单服务暂时不可用' : '还没有上墙的赞助人' }}</h4>
      <p>
        {{
          sponsorServiceUnavailable
            ? '当前页面会在后台自动重试拉取。'
            : '录入姓名、头像和金额后，这里会自动展示最新赞助名单。'
        }}
      </p>
    </div>

    <div v-else class="sponsor-layout">
      <section class="sponsor-podium" aria-label="赞助金额前三名">
        <div
          v-for="entry in podiumSponsors"
          :key="entry.slot"
          class="sponsor-podium__slot"
          :class="`sponsor-podium__slot--${entry.slot}`"
        >
          <template v-if="entry.sponsor">
            <article class="podium-card" :class="`podium-card--${entry.slot}`">
              <span class="podium-card__rank">{{ rankLabel(entry.rank) }}</span>
              <div class="podium-card__avatar">
                <img
                  v-if="entry.sponsor.avatar_url && !isAvatarBroken(entry.sponsor)"
                  :src="entry.sponsor.avatar_url"
                  :alt="entry.sponsor.name"
                  referrerpolicy="no-referrer"
                  @error="markAvatarBroken(entry.sponsor)"
                />
                <span v-else>{{ entry.sponsor.initial }}</span>
              </div>
              <div class="podium-card__name">{{ entry.sponsor.name }}</div>
              <div class="podium-card__amount">{{ formatAmount(entry.sponsor.amount) }}</div>
            </article>
            <div class="podium-base" :class="`podium-base--${entry.slot}`"></div>
          </template>
          <div v-else class="sponsor-podium__placeholder" aria-hidden="true"></div>
        </div>
      </section>

      <section v-if="remainingSponsors.length > 0" class="sponsor-grid">
        <article
          v-for="sponsor in remainingSponsors"
          :key="sponsor.id"
          class="sponsor-card"
        >
          <div class="sponsor-card__avatar">
            <img
              v-if="sponsor.avatar_url && !isAvatarBroken(sponsor)"
              :src="sponsor.avatar_url"
              :alt="sponsor.name"
              referrerpolicy="no-referrer"
              @error="markAvatarBroken(sponsor)"
            />
            <span v-else>{{ sponsor.initial }}</span>
          </div>
          <div class="sponsor-card__body">
            <div class="sponsor-card__name">{{ sponsor.name }}</div>
          </div>
          <div class="sponsor-card__amount">{{ formatAmount(sponsor.amount) }}</div>
        </article>
      </section>
    </div>
  </el-card>
</template>

<style scoped lang="scss">
.sponsor-wall {
  overflow: hidden;
  background:
    radial-gradient(circle at top right, rgba(249, 115, 22, 0.14), transparent 28%),
    radial-gradient(circle at bottom left, rgba(250, 204, 21, 0.14), transparent 25%),
    linear-gradient(135deg, rgba(255, 251, 235, 0.96), rgba(255, 247, 237, 0.92));
  border: 1px solid rgba(249, 115, 22, 0.1);
}

.sponsor-wall__header {
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
}

.card-title-bar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title-dot {
  width: 3px;
  height: 14px;
  border-radius: 2px;
  display: inline-block;
  flex-shrink: 0;
}

.title-text {
  font-size: 14px;
  font-weight: 700;
  color: #7c2d12;
}

.sponsor-wall__summary {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #7c5f10;
}

.summary-pill {
  padding: 6px 10px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.62);
  border: 1px solid rgba(249, 115, 22, 0.12);
}

.sponsor-loading {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 12px;
}

.sponsor-loading__card {
  position: relative;
  overflow: hidden;
  min-height: 88px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.7);
  border: 1px solid rgba(251, 146, 60, 0.16);

  &::after {
    content: '';
    position: absolute;
    inset: 0;
    transform: translateX(-100%);
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.72), transparent);
    animation: sponsor-loading 1.4s ease-in-out infinite;
  }
}

@keyframes sponsor-loading {
  to {
    transform: translateX(100%);
  }
}

.sponsor-empty {
  position: relative;
  padding: 28px 24px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px dashed rgba(249, 115, 22, 0.24);
  text-align: center;

  h4 {
    margin: 10px 0 8px;
    font-size: 24px;
    font-weight: 700;
    color: #7c2d12;
  }

  p {
    margin: 0 auto;
    max-width: 520px;
    font-size: 13px;
    line-height: 1.8;
    color: #9a3412;
  }
}

.sponsor-layout {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.sponsor-podium {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  grid-template-areas: 'second first third';
  gap: 12px;
  align-items: end;
}

.sponsor-podium__slot {
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  gap: 10px;
}

.sponsor-podium__slot--first {
  grid-area: first;
}

.sponsor-podium__slot--second {
  grid-area: second;
}

.sponsor-podium__slot--third {
  grid-area: third;
}

.sponsor-podium__placeholder {
  min-height: 1px;
}

.podium-card {
  position: relative;
  min-height: 144px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 18px 16px 16px;
  border-radius: 22px;
  text-align: center;
  background:
    radial-gradient(circle at top, rgba(255, 255, 255, 0.52), transparent 44%),
    rgba(255, 255, 255, 0.84);
  border: 1px solid rgba(249, 115, 22, 0.16);
  box-shadow: 0 12px 24px rgba(180, 83, 9, 0.08);
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease;

  &:hover {
    transform: translateY(-2px);
    box-shadow: 0 16px 28px rgba(180, 83, 9, 0.12);
  }
}

.podium-card--first {
  background:
    radial-gradient(circle at top, rgba(255, 255, 255, 0.56), transparent 46%),
    linear-gradient(180deg, rgba(255, 252, 235, 0.96), rgba(255, 247, 237, 0.9));
}

.podium-card__rank {
  position: absolute;
  top: 12px;
  left: 12px;
  padding: 4px 10px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.88);
  border: 1px solid rgba(249, 115, 22, 0.18);
  font-size: 11px;
  font-weight: 700;
  color: #9a3412;
}

.podium-card__avatar {
  width: 54px;
  height: 54px;
  flex-shrink: 0;

  img,
  span {
    width: 100%;
    height: 100%;
    border-radius: 18px;
    object-fit: cover;
    background: linear-gradient(135deg, #c2410c, #f97316);
    color: #fff;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 20px;
    font-weight: 700;
    box-shadow: 0 8px 16px rgba(194, 65, 12, 0.16);
  }
}

.podium-card--first .podium-card__avatar {
  width: 58px;
  height: 58px;
}

.podium-card__name {
  width: 100%;
  font-size: 15px;
  font-weight: 700;
  color: #7c2d12;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.podium-card__amount {
  font-size: 18px;
  font-weight: 800;
  color: #ea580c;
  white-space: nowrap;
}

.podium-card--first .podium-card__amount {
  font-size: 19px;
}

.podium-base {
  border-radius: 18px 18px 14px 14px;
  border: 1px solid rgba(249, 115, 22, 0.14);
  background:
    linear-gradient(180deg, rgba(255, 247, 237, 0.92), rgba(253, 230, 138, 0.72));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.6);
}

.podium-base--first {
  height: 56px;
}

.podium-base--second {
  height: 42px;
}

.podium-base--third {
  height: 34px;
}

.sponsor-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

.sponsor-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.78);
  border: 1px solid rgba(253, 186, 116, 0.18);
  box-shadow: 0 8px 18px rgba(180, 83, 9, 0.05);
}

.sponsor-card__avatar {
  width: 44px;
  height: 44px;
  flex-shrink: 0;

  img,
  span {
    width: 100%;
    height: 100%;
    border-radius: 14px;
    object-fit: cover;
    background: linear-gradient(135deg, #c2410c, #f97316);
    color: #fff;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 18px;
    font-weight: 700;
  }
}

.sponsor-card__body {
  min-width: 0;
  flex: 1;
}

.sponsor-card__name {
  font-size: 14px;
  font-weight: 700;
  color: #7c2d12;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sponsor-card__amount {
  font-size: 16px;
  font-weight: 800;
  color: #ea580c;
  white-space: nowrap;
}

@media (max-width: 768px) {
  .sponsor-podium {
    grid-template-columns: 1fr;
    grid-template-areas:
      'first'
      'second'
      'third';
  }

  .sponsor-podium__placeholder {
    display: none;
  }

  .podium-card {
    min-height: 126px;
  }

  .podium-base {
    height: 18px !important;
  }

  .sponsor-grid {
    grid-template-columns: 1fr;
  }

  .sponsor-empty {
    padding: 24px 18px;
  }

  .sponsor-card {
    align-items: flex-start;
  }
}

:global(html.dark) {
  .sponsor-wall {
    background:
      radial-gradient(circle at top right, rgba(249, 115, 22, 0.12), transparent 28%),
      radial-gradient(circle at bottom left, rgba(250, 204, 21, 0.1), transparent 25%),
      linear-gradient(135deg, rgba(30, 41, 59, 0.96), rgba(15, 23, 42, 0.94));
    border-color: rgba(249, 115, 22, 0.18);
  }

  .title-text,
  .sponsor-wall__summary,
  .sponsor-empty h4,
  .sponsor-empty p,
  .podium-card__name,
  .sponsor-card__name {
    color: #f8fafc;
  }

  .summary-pill,
  .sponsor-loading__card,
  .sponsor-empty,
  .podium-card,
  .sponsor-card,
  .podium-card__rank {
    background: color-mix(in srgb, var(--el-bg-color-overlay) 92%, black);
    border-color: rgba(249, 115, 22, 0.18);
  }

  .podium-base {
    background: linear-gradient(180deg, rgba(71, 85, 105, 0.9), rgba(51, 65, 85, 0.88));
    border-color: rgba(249, 115, 22, 0.16);
  }
}
</style>
