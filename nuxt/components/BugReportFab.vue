<template>
  <div v-if="visible">
    <button
      class="bug-fab"
      type="button"
      title="Сообщить о проблеме"
      @click="open"
    >
      <Icon name="lucide:bug" />
    </button>

    <Teleport to="body">
      <div v-if="drawerOpen" class="bug-overlay" @click="close" />
      <aside v-if="drawerOpen" class="bug-drawer" role="dialog" aria-label="Сообщить о проблеме">
        <header class="bug-head">
          <h2>Сообщить о проблеме</h2>
          <button class="btn btn-ghost btn-sm" @click="close" aria-label="Закрыть">
            <Icon name="lucide:x" />
          </button>
        </header>

        <div class="bug-body">
          <label class="field">
            <span class="field-label">Тип</span>
            <select v-model="type" class="select">
              <option value="bug">Ошибка</option>
              <option value="data">Неверные данные</option>
              <option value="ui">Интерфейс</option>
              <option value="performance">Производительность</option>
              <option value="other">Другое</option>
            </select>
          </label>

          <label class="field">
            <span class="field-label">Заголовок<span class="req">*</span></span>
            <input v-model="title" class="input" placeholder="Кратко, что не так" maxlength="200" />
          </label>

          <label class="field">
            <span class="field-label">Описание</span>
            <textarea v-model="description" class="textarea" rows="3"
                      placeholder="Что произошло, что ожидалось" />
          </label>

          <label class="field">
            <span class="field-label">Шаги воспроизведения</span>
            <textarea v-model="steps" class="textarea" rows="3"
                      placeholder="1. … 2. … 3. …" />
          </label>

          <div class="field">
            <div class="field-label-row">
              <span class="field-label">Скриншоты <span class="hint">(до 8 шт. × 5MB)</span></span>
              <button class="btn btn-ghost btn-sm" type="button" :disabled="busy" @click="grabScreenshot">
                <Icon name="lucide:camera" /> Снять текущую страницу
              </button>
            </div>
            <div
              class="dropzone"
              @click="filePick?.click()"
              @dragover.prevent
              @drop.prevent="onDrop"
              @paste="onPaste"
              tabindex="0"
            >
              <span v-if="!shots.length">Перетащите файлы или вставьте из буфера (Ctrl+V)</span>
              <div v-else class="shots-grid">
                <div v-for="(s, i) in shots" :key="i" class="shot">
                  <img :src="s.preview" alt="" />
                  <button class="shot-x" type="button" @click.stop="removeShot(i)" title="Удалить">
                    <Icon name="lucide:x" />
                  </button>
                </div>
              </div>
              <input ref="filePick" type="file" accept="image/png,image/jpeg,image/webp,image/gif"
                     multiple class="file-hidden" @change="onPick" />
            </div>
          </div>

          <div v-if="error" class="error">{{ error }}</div>
          <div v-if="message" class="success">{{ message }}</div>
        </div>

        <footer class="bug-foot">
          <button class="btn btn-ghost" @click="close" :disabled="busy">Отмена</button>
          <button class="btn btn-primary" :disabled="busy || !title.trim()" @click="submit">
            <Icon v-if="!busy" name="lucide:send" />
            <Icon v-else name="lucide:loader-2" class="spin" />
            Отправить
          </button>
        </footer>
      </aside>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { useBugReport } from "~/composables/useBugReport";

const route = useRoute();
const { user } = useAuth();
const config = useRuntimeConfig();

// FAB прячется в cost-only режиме (там нет общего auth) и до логина.
const visible = computed(() => {
  if ((config.public as any).costOnly) return false;
  if (!user.value) return false;
  if (route.path === "/login") return false;
  return true;
});

const drawerOpen = ref(false);
const type = ref("bug");
const title = ref("");
const description = ref("");
const steps = ref("");
const shots = ref<{ blob: Blob; preview: string }[]>([]);
const busy = ref(false);
const error = ref<string | null>(null);
const message = ref<string | null>(null);

const filePick = ref<HTMLInputElement | null>(null);

const reportApi = useBugReport();

const reset = () => {
  title.value = "";
  description.value = "";
  steps.value = "";
  shots.value.forEach((s) => URL.revokeObjectURL(s.preview));
  shots.value = [];
  error.value = null;
  message.value = null;
};

const open = () => {
  reset();
  drawerOpen.value = true;
};
const close = () => {
  drawerOpen.value = false;
};

const addBlob = (blob: Blob) => {
  if (shots.value.length >= 8) {
    error.value = "Максимум 8 файлов";
    return;
  }
  if (blob.size > 5 * 1024 * 1024) {
    error.value = "Файл больше 5MB";
    return;
  }
  if (!/^image\/(png|jpeg|webp|gif)$/.test(blob.type)) {
    error.value = "Поддерживаются только PNG, JPEG, WebP, GIF";
    return;
  }
  shots.value.push({ blob, preview: URL.createObjectURL(blob) });
};

const onPick = (e: Event) => {
  const target = e.target as HTMLInputElement;
  if (!target.files) return;
  Array.from(target.files).forEach(addBlob);
  target.value = "";
};
const onDrop = (e: DragEvent) => {
  const items = e.dataTransfer?.files;
  if (!items) return;
  Array.from(items).forEach((f) => addBlob(f));
};
const onPaste = (e: ClipboardEvent) => {
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const it of items) {
    if (it.kind === "file") {
      const f = it.getAsFile();
      if (f) addBlob(f);
    }
  }
};
const removeShot = (i: number) => {
  URL.revokeObjectURL(shots.value[i].preview);
  shots.value.splice(i, 1);
};

const grabScreenshot = async () => {
  error.value = null;
  busy.value = true;
  try {
    const blob = await reportApi.captureScreenshot();
    if (!blob) {
      error.value = "Не удалось снять скриншот (нет html2canvas?)";
      return;
    }
    addBlob(blob);
  } finally {
    busy.value = false;
  }
};

const submit = async () => {
  error.value = null;
  message.value = null;
  busy.value = true;
  try {
    const res = await reportApi.submit({
      type: type.value,
      title: title.value,
      description: description.value,
      steps: steps.value,
      screenshots: shots.value.map((s) => s.blob)
    });
    message.value = res.deduplicated
      ? `Похожий репорт уже зарегистрирован (#${res.id})`
      : `Спасибо! Репорт #${res.id} отправлен.`;
    // Не закрываем сразу — показываем подтверждение.
    setTimeout(() => {
      if (drawerOpen.value) close();
    }, 1500);
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка отправки";
  } finally {
    busy.value = false;
  }
};
</script>

<style scoped>
.bug-fab {
  position: fixed;
  right: 16px; bottom: 16px;
  width: 44px; height: 44px;
  border-radius: 50%;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  display: inline-flex; align-items: center; justify-content: center;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.10);
  cursor: pointer;
  z-index: 900;
  transition: background 0.12s, color 0.12s, transform 0.12s;
}
.bug-fab:hover { color: var(--accent); transform: translateY(-1px); }

.bug-overlay {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.32);
  z-index: 1100;
}
.bug-drawer {
  position: fixed; top: 0; right: 0; bottom: 0;
  width: 480px; max-width: 100vw;
  background: var(--bg-surface);
  border-left: 1px solid var(--border);
  z-index: 1101;
  display: flex; flex-direction: column;
  box-shadow: -2px 0 12px rgba(0, 0, 0, 0.08);
}
.bug-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
}
.bug-head h2 { margin: 0; font-size: var(--fs-md); }

.bug-body { flex: 1; padding: var(--sp-5); overflow-y: auto; }
.bug-foot {
  display: flex; gap: var(--sp-3); justify-content: flex-end;
  padding: var(--sp-4) var(--sp-5);
  border-top: 1px solid var(--border);
}

.field { display: block; margin-bottom: var(--sp-4); }
.field-label {
  display: block;
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}
.field-label-row { display: flex; align-items: center; justify-content: space-between; }
.req { color: var(--neg-strong); margin-left: 2px; }
.hint { color: var(--text-muted); text-transform: none; letter-spacing: 0; font-weight: var(--fw-normal); }
.input, .select, .textarea {
  width: 100%; padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-base);
  color: var(--text-strong);
  font: inherit;
}
.textarea { resize: vertical; }

.dropzone {
  border: 1px dashed var(--border);
  border-radius: var(--rd-3);
  padding: var(--sp-4);
  text-align: center;
  color: var(--text-muted);
  cursor: pointer;
  min-height: 72px;
}
.file-hidden { display: none; }
.shots-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-3); }
.shot {
  position: relative;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  overflow: hidden;
  aspect-ratio: 1;
  background: var(--bg-subtle);
}
.shot img { width: 100%; height: 100%; object-fit: cover; }
.shot-x {
  position: absolute;
  top: 2px; right: 2px;
  background: rgba(0, 0, 0, 0.6);
  color: white;
  border: none;
  border-radius: 50%;
  width: 18px; height: 18px;
  display: inline-flex; align-items: center; justify-content: center;
  cursor: pointer;
}

.error { color: var(--neg-strong); background: var(--neg-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.success { color: var(--pos-strong); background: var(--pos-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
