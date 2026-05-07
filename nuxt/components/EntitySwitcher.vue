<template>
  <div class="entity-switcher" :class="{ open: isOpen }" v-click-outside="close">
    <button class="entity-trigger" @click="toggle" :aria-expanded="isOpen">
      <span class="entity-flag">{{ countryLabel?.flag || '🌐' }}</span>
      <span class="entity-text">
        <span class="entity-text-strong">{{ label }}</span>
        <span class="entity-text-sub">
          <template v-if="selection.kind === 'entity'">
            ИНН {{ entityInn }}
          </template>
          <template v-else-if="selection.kind === 'country'">
            {{ countryEntitiesCount }} юр.лиц
          </template>
          <template v-else>
            {{ ENTITIES.length }} юр.лиц · {{ COUNTRIES.length }} страны
          </template>
        </span>
      </span>
      <Icon name="lucide:chevrons-up-down" class="entity-chev" />
    </button>

    <Transition name="popover">
      <div v-if="isOpen" class="entity-popover" role="menu">
        <div class="entity-search">
          <Icon name="lucide:search" class="entity-search-icon" />
          <input
            ref="searchEl"
            v-model="q"
            class="entity-search-input"
            placeholder="Поиск компании или ИНН…"
            @keydown.esc="close"
          />
        </div>

        <div class="entity-section">
          <button
            class="entity-row entity-row-all"
            :class="{ active: selection.kind === 'all' }"
            @click="pick({ kind: 'all' })"
          >
            <span class="entity-row-icon">
              <Icon name="lucide:layers" />
            </span>
            <span class="entity-row-main">
              <span class="entity-row-name">Вся группа компаний</span>
              <span class="entity-row-meta">{{ ENTITIES.length }} юр.лиц · консолидированно</span>
            </span>
            <Icon v-if="selection.kind === 'all'" name="lucide:check" class="entity-row-check" />
          </button>
        </div>

        <div v-for="country in COUNTRIES" :key="country.code" class="entity-section">
          <div class="entity-section-header">
            <span class="entity-flag">{{ country.flag }}</span>
            <span class="entity-section-name">{{ country.name }}</span>
            <button
              class="entity-section-all"
              :class="{ active: selection.kind === 'country' && selection.code === country.code }"
              @click="pick({ kind: 'country', code: country.code })"
            >
              Все юр.лица
              <Icon
                v-if="selection.kind === 'country' && selection.code === country.code"
                name="lucide:check"
              />
            </button>
          </div>

          <button
            v-for="ent in entitiesIn(country.code)"
            :key="ent.id"
            class="entity-row"
            :class="{ active: selection.kind === 'entity' && selection.id === ent.id, inactive: !ent.active }"
            :disabled="!ent.active"
            @click="pick({ kind: 'entity', id: ent.id })"
          >
            <span class="entity-row-form">{{ ent.legalForm }}</span>
            <span class="entity-row-main">
              <span class="entity-row-name">{{ ent.name }}</span>
              <span class="entity-row-meta">ИНН {{ ent.inn }}</span>
            </span>
            <span v-if="!ent.active" class="badge badge-warn">Не активно</span>
            <Icon
              v-if="selection.kind === 'entity' && selection.id === ent.id"
              name="lucide:check"
              class="entity-row-check"
            />
          </button>
        </div>

        <div class="entity-footer">
          <span>Контекст влияет на все цифры в кабинете</span>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick } from "vue";
import { useEntity, COUNTRIES, ENTITIES, type Selection } from "~/composables/useEntity";

const { selection, setSelection, label, countryLabel } = useEntity();

const isOpen = ref(false);
const q = ref("");
const searchEl = ref<HTMLInputElement | null>(null);

const toggle = async () => {
  isOpen.value = !isOpen.value;
  if (isOpen.value) {
    await nextTick();
    searchEl.value?.focus();
  }
};
const close = () => { isOpen.value = false; q.value = ""; };
const pick = (s: Selection) => {
  setSelection(s);
  close();
};

const entityInn = computed(() => {
  if (selection.value.kind !== "entity") return "";
  return ENTITIES.find((e) => e.id === selection.value.id)?.inn || "";
});

const countryEntitiesCount = computed(() => {
  if (selection.value.kind !== "country") return 0;
  return ENTITIES.filter((e) => e.countryCode === selection.value.code).length;
});

const entitiesIn = (code: string) => {
  const ql = q.value.trim().toLowerCase();
  return ENTITIES.filter((e) => e.countryCode === code).filter((e) => {
    if (!ql) return true;
    return `${e.name} ${e.inn} ${e.legalForm}`.toLowerCase().includes(ql);
  });
};

// === click-outside директива (минимальная) ===
const vClickOutside = {
  mounted(el: HTMLElement, binding: any) {
    el._handler = (e: MouseEvent) => {
      if (!el.contains(e.target as Node)) binding.value();
    };
    document.addEventListener("click", el._handler, true);
  },
  unmounted(el: HTMLElement) {
    document.removeEventListener("click", el._handler, true);
  }
};
</script>

<style scoped>
.entity-switcher {
  position: relative;
  display: inline-flex;
}

.entity-trigger {
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
  cursor: pointer;
  transition: border-color var(--t-fast), background var(--t-fast);
  max-width: 320px;
}
.entity-trigger:hover {
  border-color: var(--border-strong);
  background: var(--bg-surface-2);
}
.entity-switcher.open .entity-trigger {
  border-color: var(--accent);
  box-shadow: var(--shadow-focus);
}

.entity-flag {
  font-size: 18px;
  line-height: 1;
  flex-shrink: 0;
  filter: saturate(1.1);
}

.entity-text {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0;
  line-height: 1.15;
  min-width: 0;
}
.entity-text-strong {
  font-size: var(--fs-sm);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 220px;
}
.entity-text-sub {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  font-family: var(--font-mono);
  white-space: nowrap;
}

.entity-chev {
  width: 14px;
  height: 14px;
  color: var(--text-muted);
  flex-shrink: 0;
}

/* === Popover === */
.entity-popover {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: var(--z-dropdown);
  width: 360px;
  max-height: 70vh;
  overflow-y: auto;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  box-shadow: var(--shadow-pop);
  padding: var(--sp-3);
}

.entity-search {
  position: sticky;
  top: 0;
  background: var(--bg-elevated);
  padding: var(--sp-3) var(--sp-3) var(--sp-4);
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  border-bottom: 1px solid var(--border);
  margin: -3px -3px 0;
  padding: var(--sp-4) var(--sp-5);
}
.entity-search-icon {
  width: 14px;
  height: 14px;
  color: var(--text-muted);
  flex-shrink: 0;
}
.entity-search-input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  font-size: var(--fs-sm);
  color: var(--text-primary);
  font-family: var(--font-sans);
}
.entity-search-input::placeholder { color: var(--text-muted); }

.entity-section {
  padding: var(--sp-3) 0;
  border-bottom: 1px solid var(--border);
}
.entity-section:last-of-type { border-bottom: none; }

.entity-section-header {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding: var(--sp-3) var(--sp-4) var(--sp-2);
  font-size: var(--fs-2xs);
  font-weight: var(--fw-bold);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
}
.entity-section-header .entity-flag { font-size: 14px; }
.entity-section-name { flex: 1; }
.entity-section-all {
  background: transparent;
  border: 1px solid transparent;
  padding: 2px 6px;
  font-size: var(--fs-2xs);
  font-weight: var(--fw-semibold);
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: var(--rd-3);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  text-transform: none;
  letter-spacing: normal;
  font-family: inherit;
}
.entity-section-all:hover {
  background: var(--bg-surface-3);
  color: var(--text-strong);
}
.entity-section-all.active {
  background: var(--accent-soft);
  color: var(--accent);
}
.entity-section-all :deep(svg) { width: 12px; height: 12px; }

.entity-row {
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
.entity-row:hover:not(:disabled) {
  background: var(--bg-surface-3);
}
.entity-row.active {
  background: var(--accent-soft);
}
.entity-row.inactive {
  opacity: 0.55;
  cursor: not-allowed;
}

.entity-row-icon {
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
.entity-row-icon :deep(svg) { width: 14px; height: 14px; }

.entity-row-form {
  width: 36px;
  flex-shrink: 0;
  font-size: var(--fs-2xs);
  font-weight: var(--fw-bold);
  text-align: center;
  color: var(--text-muted);
  background: var(--bg-surface-3);
  padding: 4px 6px;
  border-radius: var(--rd-3);
  letter-spacing: 0.04em;
}

.entity-row-main {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.entity-row-name {
  font-size: var(--fs-base);
  font-weight: var(--fw-medium);
  color: var(--text-strong);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.entity-row.active .entity-row-name { color: var(--accent); }
.entity-row-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.entity-row-check {
  width: 14px;
  height: 14px;
  color: var(--accent);
  flex-shrink: 0;
}

.entity-footer {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  text-align: center;
  padding: var(--sp-4);
  border-top: 1px solid var(--border);
  margin: 0 -3px -3px;
}

/* Анимация поповера */
.popover-enter-active,
.popover-leave-active { transition: opacity 0.12s ease, transform 0.12s ease; }
.popover-enter-from,
.popover-leave-to { opacity: 0; transform: translateY(-4px); }

@media (max-width: 768px) {
  .entity-popover {
    position: fixed;
    top: 56px;
    left: 8px;
    right: 8px;
    width: auto;
  }
  .entity-text-sub { display: none; }
}
</style>
