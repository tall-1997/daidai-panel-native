<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    variant?: "infinity" | "spinner" | "dots";
    size?: "sm" | "md" | "lg";
    label?: string;
    tone?: "primary" | "neutral" | "warning";
    stacked?: boolean;
  }>(),
  {
    variant: "infinity",
    size: "md",
    label: "",
    tone: "primary",
    stacked: true,
  },
);

const wrapperClass = computed(() => [
  "dd-loading-motion",
  `dd-loading-motion--${props.variant}`,
  `dd-loading-motion--${props.size}`,
  `dd-loading-motion--${props.tone}`,
  { "is-stacked": props.stacked, "has-label": !!props.label },
]);
</script>

<template>
  <div :class="wrapperClass" role="status" aria-live="polite">
    <svg
      v-if="variant === 'infinity'"
      class="dd-loading-motion__svg dd-loading-motion__svg--infinity"
      viewBox="0 0 120 48"
      fill="none"
      aria-hidden="true"
    >
      <path
        class="track"
        d="M24 24C24 15.1634 31.1634 8 40 8C54 8 54 40 68 40C76.8366 40 84 32.8366 84 24C84 15.1634 91.1634 8 100 8C108.837 8 116 15.1634 116 24C116 32.8366 108.837 40 100 40C86 40 86 8 72 8C63.1634 8 56 15.1634 56 24C56 32.8366 48.8366 40 40 40C31.1634 40 24 32.8366 24 24Z"
      />
      <path
        class="accent accent--front"
        d="M24 24C24 15.1634 31.1634 8 40 8C54 8 54 40 68 40C76.8366 40 84 32.8366 84 24C84 15.1634 91.1634 8 100 8"
        pathLength="100"
      />
      <path
        class="accent accent--back"
        d="M116 24C116 32.8366 108.837 40 100 40C86 40 86 8 72 8C63.1634 8 56 15.1634 56 24C56 32.8366 48.8366 40 40 40C31.1634 40 24 32.8366 24 24"
        pathLength="100"
      />
    </svg>

    <svg
      v-else-if="variant === 'spinner'"
      class="dd-loading-motion__svg dd-loading-motion__svg--spinner"
      viewBox="0 0 40 40"
      fill="none"
      aria-hidden="true"
    >
      <circle class="track" cx="20" cy="20" r="15" />
      <circle class="accent" cx="20" cy="20" r="15" pathLength="100" />
    </svg>

    <svg
      v-else
      class="dd-loading-motion__svg dd-loading-motion__svg--dots"
      viewBox="0 0 64 24"
      fill="none"
      aria-hidden="true"
    >
      <circle class="dot dot--1" cx="12" cy="12" r="5" />
      <circle class="dot dot--2" cx="32" cy="12" r="5" />
      <circle class="dot dot--3" cx="52" cy="12" r="5" />
    </svg>

    <span v-if="label" class="dd-loading-motion__label">{{ label }}</span>
  </div>
</template>

<style scoped lang="scss">
.dd-loading-motion {
  --dd-loading-color: var(--el-color-primary);
  --dd-loading-soft-color: color-mix(
    in srgb,
    var(--dd-loading-color) 18%,
    transparent
  );
  --dd-loading-glow-color: color-mix(
    in srgb,
    var(--dd-loading-color) 32%,
    transparent
  );
  --dd-loading-text-color: var(--el-text-color-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;

  &.is-stacked {
    flex-direction: column;
  }

  &--neutral {
    --dd-loading-color: color-mix(
      in srgb,
      var(--el-text-color-secondary) 92%,
      transparent
    );
  }

  &--warning {
    --dd-loading-color: var(--el-color-warning);
  }
}

.dd-loading-motion__label {
  font-size: 13px;
  line-height: 1.5;
  color: var(--dd-loading-text-color);
}

.dd-loading-motion__svg {
  flex-shrink: 0;
  display: block;
  overflow: visible;
  filter: drop-shadow(0 4px 14px var(--dd-loading-glow-color));
}

.dd-loading-motion__svg .track,
.dd-loading-motion__svg .accent,
.dd-loading-motion__svg .dot {
  vector-effect: non-scaling-stroke;
}

.dd-loading-motion--sm {
  .dd-loading-motion__svg--spinner {
    width: 16px;
    height: 16px;
  }

  .dd-loading-motion__svg--dots {
    width: 28px;
    height: 12px;
  }

  .dd-loading-motion__svg--infinity {
    width: 34px;
    height: 14px;
  }

  .dd-loading-motion__label {
    font-size: 12px;
  }
}

.dd-loading-motion--md {
  .dd-loading-motion__svg--spinner {
    width: 20px;
    height: 20px;
  }

  .dd-loading-motion__svg--dots {
    width: 36px;
    height: 16px;
  }

  .dd-loading-motion__svg--infinity {
    width: 46px;
    height: 18px;
  }
}

.dd-loading-motion--lg {
  .dd-loading-motion__svg--spinner {
    width: 28px;
    height: 28px;
  }

  .dd-loading-motion__svg--dots {
    width: 50px;
    height: 20px;
  }

  .dd-loading-motion__svg--infinity {
    width: 64px;
    height: 24px;
  }

  .dd-loading-motion__label {
    font-size: 14px;
  }
}

.dd-loading-motion__svg--spinner {
  transform-origin: center;
  animation: dd-loading-spinner-rotate 1s linear infinite;

  .track {
    stroke: color-mix(in srgb, var(--dd-loading-color) 14%, transparent);
    stroke-width: 4;
  }

  .accent {
    stroke: var(--dd-loading-color);
    stroke-width: 4;
    stroke-linecap: round;
    stroke-dasharray: 24 76;
    animation: dd-loading-spinner-dash 1.2s ease-in-out infinite;
    transform-origin: center;
  }
}

.dd-loading-motion__svg--dots {
  .dot {
    fill: var(--dd-loading-color);
    transform-origin: center;
    animation: dd-loading-dot-breathe 1.15s ease-in-out infinite;
  }

  .dot--2 {
    animation-delay: 0.12s;
  }

  .dot--3 {
    animation-delay: 0.24s;
  }
}

.dd-loading-motion__svg--infinity {
  .track {
    stroke: color-mix(in srgb, var(--dd-loading-color) 14%, transparent);
    stroke-width: 6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .accent {
    stroke: var(--dd-loading-color);
    stroke-width: 6;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 38 62;
  }

  .accent--front {
    animation: dd-loading-infinity-front 1.35s ease-in-out infinite;
  }

  .accent--back {
    animation: dd-loading-infinity-back 1.35s ease-in-out infinite;
  }
}

@keyframes dd-loading-spinner-rotate {
  to {
    transform: rotate(360deg);
  }
}

@keyframes dd-loading-spinner-dash {
  0% {
    stroke-dasharray: 18 82;
    stroke-dashoffset: 0;
  }
  50% {
    stroke-dasharray: 38 62;
    stroke-dashoffset: -14;
  }
  100% {
    stroke-dasharray: 18 82;
    stroke-dashoffset: -48;
  }
}

@keyframes dd-loading-dot-breathe {
  0%,
  80%,
  100% {
    opacity: 0.28;
    transform: translateY(0) scale(0.78);
  }
  40% {
    opacity: 1;
    transform: translateY(-1px) scale(1);
  }
}

@keyframes dd-loading-infinity-front {
  0% {
    stroke-dashoffset: 0;
    opacity: 0.9;
  }
  50% {
    stroke-dashoffset: -52;
    opacity: 1;
  }
  100% {
    stroke-dashoffset: -104;
    opacity: 0.9;
  }
}

@keyframes dd-loading-infinity-back {
  0% {
    stroke-dashoffset: -28;
    opacity: 0.45;
  }
  50% {
    stroke-dashoffset: -80;
    opacity: 0.9;
  }
  100% {
    stroke-dashoffset: -132;
    opacity: 0.45;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dd-loading-motion__svg--spinner,
  .dd-loading-motion__svg--spinner .accent,
  .dd-loading-motion__svg--dots .dot,
  .dd-loading-motion__svg--infinity .accent {
    animation: none !important;
  }
}
</style>
