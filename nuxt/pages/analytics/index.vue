<template>
  <div class="page-analytics">
    <header class="page-header">
      <div>
        <h1 class="page-title">Аналитика</h1>
        <p class="page-subtitle">Произвольные запросы через Python-песочницу аналитика</p>
      </div>
      <div class="page-actions">
        <span class="badge badge-warn"><span class="badge-dot"></span>Sandbox · read-only</span>
      </div>
    </header>

    <div class="card">
      <div class="card-body empty">
        <Icon name="lucide:flask-conical" class="empty-icon" />
        <div class="empty-title">Раздел готовится</div>
        <div class="empty-hint">
          Сюда подключим интерфейс к Python-контейнеру с правом SELECT-only
          на основные таблицы и собственной схемой <code>analytics</code>.
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ middleware: "scope-guard" });
</script>
