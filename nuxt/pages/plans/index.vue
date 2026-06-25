<template>
  <div class="page-plans">
    <header class="page-header">
      <div>
        <h1 class="page-title">Тактические планы</h1>
        <p class="page-subtitle">Формирование и согласование тактических планов P&amp;L</p>
      </div>
    </header>

    <!--
      VS0 — каркас раздела. Дашборд задач по этапам/р.д. и форма «Маркетплейсы»
      (TPL-MP) приходят следующими срезами VS1+. См. docs/reports/plans/plan.md.
    -->
    <section class="card placeholder">
      <Icon name="lucide:clipboard-list" class="placeholder-icon" />
      <p>Раздел в разработке. Скоро здесь появятся формы ввода и согласование планов.</p>
      <div class="placeholder-actions">
        <NuxtLink to="/plans/mp" class="btn btn-ghost">
          <Icon name="lucide:store" /> Маркетплейсы — факт
        </NuxtLink>
        <NuxtLink to="/plans/mp/large" class="btn btn-primary">
          <Icon name="lucide:pencil" /> Маркетплейсы — ввод тактики
        </NuxtLink>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ middleware: "scope-guard" });
</script>

<style scoped>
.page-header {
  margin-bottom: var(--sp-6);
}
.placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-8, 32px) var(--sp-6);
  color: var(--fg-muted, #6b7280);
  text-align: center;
}
.placeholder-icon {
  width: 32px;
  height: 32px;
  opacity: 0.5;
}
</style>
