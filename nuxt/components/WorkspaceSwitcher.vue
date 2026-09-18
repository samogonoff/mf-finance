<template>
  <div v-if="workspaces.length > 1" class="ws-switcher" :class="{ open: isOpen }" v-click-outside="close">
    <button class="ws-trigger" @click="toggle" :aria-expanded="isOpen" aria-haspopup="menu">
      <span class="ws-trigger-icon"><Icon :name="current.icon" /></span>
      <span class="ws-text">
        <span class="ws-text-strong">{{ current.label }}</span>
        <span class="ws-text-sub">{{ current.hint }}</span>
      </span>
      <Icon name="lucide:chevrons-up-down" class="ws-chev" />
    </button>

    <Transition name="popover">
      <div v-if="isOpen" class="ws-popover" role="menu">
        <button
          v-for="w in workspaces"
          :key="w.key"
          class="ws-row"
          :class="{ active: w.key === current.key }"
          role="menuitem"
          @click="pick(w)"
        >
          <span class="ws-row-icon"><Icon :name="w.icon" /></span>
          <span class="ws-row-main">
            <span class="ws-row-name">{{ w.label }}</span>
            <span class="ws-row-meta">{{ w.hint }}</span>
          </span>
          <Icon v-if="w.key === current.key" name="lucide:check" class="ws-row-check" />
        </button>
      </div>
    </Transition>
  </div>

  <!-- Раздел один — статичная метка без выпадашки (SHELL-CONTRACT.md §2.1). -->
  <div v-else class="ws-static">
    <span class="ws-trigger-icon"><Icon :name="current.icon" /></span>
    <span class="ws-text-strong">{{ current.label }}</span>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useWorkspaces, type Workspace } from "~/composables/useWorkspaces";

const { workspaces, current, go } = useWorkspaces();

const isOpen = ref(false);
const toggle = () => { isOpen.value = !isOpen.value; };
const close = () => { isOpen.value = false; };
const pick = (w: Workspace) => {
  close();
  go(w);
};

// click-outside: та же минимальная директива, что в EntitySwitcher.vue —
// общего плагина директив в проекте нет.
const vClickOutside = {
  mounted(el: HTMLElement, binding: any) {
    (el as any)._handler = (e: MouseEvent) => {
      if (!el.contains(e.target as Node)) binding.value();
    };
    document.addEventListener("click", (el as any)._handler, true);
  },
  unmounted(el: HTMLElement) {
    document.removeEventListener("click", (el as any)._handler, true);
  }
};
</script>

<style scoped>
.ws-switcher {
  position: relative;
  display: inline-flex;
}

.ws-trigger,
.ws-static {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-4);
  padding: 0 var(--sp-4) 0 var(--sp-3);
  height: 36px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-4);
  font-family: var(--font-sans);
  font-size: var(--fs-sm);
  color: var(--text-strong);
  max-width: 320px;
}
.ws-trigger { cursor: pointer; transition: border-color var(--t-fast), background var(--t-fast); }
.ws-trigger:hover {
  border-color: var(--border-strong);
  background: var(--bg-surface-2);
}
.ws-switcher.open .ws-trigger {
  border-color: var(--accent);
  box-shadow: var(--shadow-focus);
}

.ws-trigger-icon {
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--accent);
  flex-shrink: 0;
}
.ws-trigger-icon :deep(svg) { width: 16px; height: 16px; }

.ws-text {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.15;
  min-width: 0;
}
.ws-text-strong {
  font-size: var(--fs-sm);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
}
.ws-text-sub {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
}

.ws-chev {
  width: 14px;
  height: 14px;
  color: var(--text-muted);
  flex-shrink: 0;
}

.ws-popover {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: var(--z-dropdown);
  width: 320px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  box-shadow: var(--shadow-pop);
  padding: var(--sp-3);
}

.ws-row {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-4);
  width: 100%;
  background: transparent;
  border: none;
  text-align: left;
  cursor: pointer;
  border-radius: var(--rd-3);
  font-family: inherit;
  color: var(--text-primary);
  transition: background var(--t-fast);
}
.ws-row:hover { background: var(--bg-surface-3); }
.ws-row.active { background: var(--accent-soft); }

.ws-row-icon {
  width: 28px;
  height: 28px;
  background: var(--bg-surface-3);
  border-radius: var(--rd-3);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--text-secondary);
  flex-shrink: 0;
}
.ws-row-icon :deep(svg) { width: 14px; height: 14px; }

.ws-row-main {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.ws-row-name {
  font-size: var(--fs-base);
  font-weight: var(--fw-medium);
  color: var(--text-strong);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-row.active .ws-row-name { color: var(--accent); }
.ws-row-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-row-check {
  width: 14px;
  height: 14px;
  color: var(--accent);
  flex-shrink: 0;
}

.popover-enter-active,
.popover-leave-active { transition: opacity var(--t-fast), transform var(--t-fast); }
.popover-enter-from,
.popover-leave-to { opacity: 0; transform: translateY(-4px); }

@media (max-width: 720px) {
  .ws-text-sub { display: none; }
  .ws-trigger, .ws-static { max-width: 200px; }
}
</style>
