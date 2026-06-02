<template>
  <div class="page-reports">
    <header class="page-header">
      <div>
        <h1 class="page-title">Задолженность ВГО</h1>
        <p class="page-subtitle">Внутригрупповая задолженность по юрлицам ГК</p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost"><Icon name="lucide:printer" /> Печать</button>
        <button class="btn btn-ghost"><Icon name="lucide:download" /> Excel</button>
        <button class="btn btn-primary"><Icon name="lucide:share-2" /> Поделиться</button>
      </div>
    </header>

    <!-- Единственный отчёт кабинета — параметризованный, с многоуровневой группировкой. -->
    <DebtReport />
  </div>
</template>

<script setup lang="ts">
import DebtReport from "~/components/reports/DebtReport.vue";

definePageMeta({ middleware: "scope-guard" });
</script>
