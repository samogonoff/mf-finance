<template>
  <div class="login-shell">
    <LoginCard />
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: false });
</script>

<style scoped>
.login-shell {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-7);
  background: var(--bg-app);
}
</style>
