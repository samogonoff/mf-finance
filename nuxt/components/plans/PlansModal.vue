<template>
  <Teleport to="body">
    <Transition name="pm">
      <div v-if="open" class="pm-overlay" @click.self="emit('close')" @keydown.esc="emit('close')">
        <div class="pm-dialog" :style="{ maxWidth: width }" role="dialog" aria-modal="true">
          <header class="pm-head">
            <div>
              <h3 class="pm-title">{{ title }}</h3>
              <p v-if="subtitle" class="pm-subtitle">{{ subtitle }}</p>
            </div>
            <button class="btn btn-icon" type="button" aria-label="Закрыть" @click="emit('close')">
              <Icon name="lucide:x" />
            </button>
          </header>
          <div class="pm-body"><slot /></div>
          <footer v-if="$slots.footer" class="pm-foot"><slot name="footer" /></footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{ open: boolean; title?: string; subtitle?: string; width?: string }>(),
  { title: "", subtitle: "", width: "440px" }
);
const emit = defineEmits<{ (e: "close"): void }>();
</script>

<style scoped>
.pm-overlay {
  position: fixed;
  inset: 0;
  background: var(--bg-overlay);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 10vh var(--sp-6) var(--sp-6);
  z-index: 1000;
  backdrop-filter: blur(2px);
}
.pm-dialog {
  width: 100%;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--rd-6, 10px);
  box-shadow: var(--shadow-modal);
  overflow: hidden;
}
.pm-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--sp-5);
  padding: var(--sp-6);
  border-bottom: 1px solid var(--border);
}
.pm-title {
  font-size: var(--fs-lg);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  margin: 0;
}
.pm-subtitle {
  font-size: var(--fs-sm);
  color: var(--text-muted);
  margin: 2px 0 0;
}
.pm-body {
  padding: var(--sp-6);
}
.pm-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-4);
  padding: var(--sp-5) var(--sp-6);
  border-top: 1px solid var(--border);
  background: var(--bg-surface-2);
}
.pm-enter-active,
.pm-leave-active {
  transition: opacity 0.15s ease;
}
.pm-enter-from,
.pm-leave-to {
  opacity: 0;
}
</style>
