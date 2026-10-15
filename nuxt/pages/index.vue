<!--
  Главная кабинета — обзор разделов, которые работают на живых данных.
  До запуска здесь был дашборд на моковых цифрах (useEntity.mockMultiplier); к
  запуску он убран вместе с моковыми «Операциями», «Контрагентами» и «Аналитикой».
  Показываем только разделы, доступные пользователю по ролям.
-->
<template>
  <div class="page-home">
    <header class="page-header">
      <div>
        <h1 class="page-title">Финансы</h1>
        <p class="page-subtitle">Разделы кабинета, доступные вам</p>
      </div>
    </header>

    <div v-if="sections.length" class="home-grid">
      <component
        :is="s.external ? 'a' : NuxtLink"
        v-for="s in sections"
        :key="s.key"
        v-bind="s.external ? { href: s.to } : { to: s.to }"
        class="home-card"
      >
        <span class="home-icon"><Icon :name="s.icon" /></span>
        <span class="home-text">
          <span class="home-title">{{ s.title }}</span>
          <span class="home-desc">{{ s.desc }}</span>
        </span>
        <Icon name="lucide:arrow-right" class="home-arrow" />
      </component>
    </div>

    <div v-else class="card">
      <div class="card-body empty">
        <Icon name="lucide:lock" class="empty-icon" />
        <div class="empty-title">Нет доступных разделов</div>
        <div class="empty-hint">Доступ к разделам выдаёт администратор кабинета.</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { resolveComponent } from "vue";

definePageMeta({ middleware: "scope-guard" });

const NuxtLink = resolveComponent("NuxtLink");
const config = useRuntimeConfig();
const { hasScope } = useScope();

interface Section { key: string; title: string; desc: string; to: string; icon: string; external?: boolean; show: boolean }

const sections = computed(() => ([
  {
    key: "vgo",
    title: "Задолженность ВГО",
    desc: "Внутригрупповая задолженность по юрлицам ГК: группировки, валюты, выгрузка",
    to: "/reports",
    icon: "lucide:file-bar-chart-2",
    show: hasScope("finance"),
  },
  {
    key: "plans",
    title: "Тактические планы",
    desc: "Планы МП и розницы, затраты ЦФО, согласование и факт",
    to: "/plans",
    icon: "lucide:clipboard-list",
    show: hasScope("plans"),
  },
  {
    key: "budget",
    title: "Бюджетирование",
    desc: "Бюджеты и план-факт",
    to: "/budget/",
    icon: "lucide:landmark",
    external: true,
    show: !!config.public.budgetEnabled && hasScope("budget"),
  },
  {
    key: "strategy",
    title: "Стратегические планы",
    desc: "Доли статей прямых затрат площадок МП по месяцам",
    to: "/strategy",
    icon: "lucide:target",
    show: hasScope("plans"),
  },
  {
    key: "cost",
    title: "Себестоимость",
    desc: "Калькуляции, цены, согласования",
    to: "/cost",
    icon: "lucide:package",
    show: hasScope("cost"),
  },
] as Section[]).filter((s) => s.show));
</script>

<style scoped>
.home-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
  gap: var(--sp-5);
}
.home-card {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-6);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  color: var(--text-primary);
  text-decoration: none;
  transition: border-color var(--t-fast), background var(--t-fast);
}
.home-card:hover {
  border-color: var(--border-strong);
  background: var(--bg-surface-2);
}
.home-icon {
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: var(--rd-4);
  background: var(--accent-soft);
  color: var(--accent);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}
.home-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
.home-title { font-size: var(--fs-md); font-weight: var(--fw-semibold); color: var(--text-strong); }
.home-desc { font-size: var(--fs-sm); color: var(--text-secondary); }
.home-arrow { color: var(--text-muted); flex: none; }
</style>
