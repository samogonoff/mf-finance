<template>
  <div v-if="visible" data-html2canvas-ignore>
    <button
      class="bug-fab"
      type="button"
      title="Сообщить о проблеме"
      :disabled="capturing"
      @click="open"
    >
      <Icon v-if="capturing" name="lucide:loader-2" class="spin" />
      <Icon v-else name="lucide:bug" />
    </button>

    <Teleport to="body">
      <div v-if="drawerOpen" class="bug-overlay" data-html2canvas-ignore @click="close" />
      <aside v-if="drawerOpen" class="bug-drawer" data-html2canvas-ignore role="dialog"
             aria-label="Сообщить о проблеме" @paste="onPaste">
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
            <textarea v-model="description" class="textarea" rows="3" maxlength="5000"
                      placeholder="Что произошло, что вы делали, что ожидали" />
          </label>

          <label class="field">
            <span class="field-label">Шаги воспроизведения</span>
            <textarea v-model="steps" class="textarea" rows="3" maxlength="2000"
                      placeholder="1. … 2. … 3. …" />
          </label>

          <div class="field">
            <div class="field-label-row">
              <span class="field-label">Скриншоты <span class="hint">(до 8 шт. × 5MB)</span></span>
            </div>
            <div
              class="dropzone"
              :class="{ 'is-drag': isDrag }"
              @click="filePick?.click()"
              @dragover.prevent="isDrag = true"
              @dragleave.prevent="isDrag = false"
              @drop.prevent="onDrop"
              tabindex="0"
            >
              <span v-if="!shots.length">Перетащите файлы, вставьте из буфера (Ctrl+V) или нажмите для выбора</span>
              <div v-else class="shots-grid">
                <div v-for="(s, i) in shots" :key="i" class="shot">
                  <img :src="s.preview" alt="" />
                  <span v-if="s.auto" class="shot-auto" title="Снимок страницы в момент открытия формы">авто</span>
                  <button class="shot-x" type="button" @click.stop="removeShot(i)" title="Удалить">
                    <Icon name="lucide:x" />
                  </button>
                </div>
              </div>
              <input ref="filePick" type="file" accept="image/png,image/jpeg,image/webp,image/gif"
                     multiple class="file-hidden" @change="onPick" />
            </div>
          </div>

          <details class="bug-meta">
            <summary>Что отправится вместе с репортом</summary>
            <ul>
              <li>Страница: <code>{{ currentUrl }}</code></li>
              <li>Раздел: <code>{{ section }}</code></li>
              <li>Логи консоли (warn/error): <code>{{ stats.console }}</code></li>
              <li>Запросы к API (всего / с ошибкой): <code>{{ stats.network }} / {{ stats.networkErrors }}</code></li>
              <li>JS-ошибки: <code>{{ stats.js }}</code></li>
              <li>Автоснимок страницы: <code>{{ hasAutoShot ? "да" : "нет" }}</code></li>
              <li>Браузер, экран, часовой пояс</li>
            </ul>
          </details>

          <div v-if="error" class="error">{{ error }}</div>
          <div v-if="message" class="success">{{ message }}</div>
        </div>

        <footer class="bug-foot">
          <button class="btn btn-ghost" @click="close" :disabled="busy">Отмена</button>
          <button class="btn btn-primary" :disabled="busy || !title.trim()" @click="submit">
            <Icon v-if="!busy" name="lucide:send" />
            <Icon v-else name="lucide:loader-2" class="spin" />
            {{ busy ? "Отправляем…" : "Отправить" }}
          </button>
        </footer>
      </aside>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { useBugReport, detectSection } from "~/composables/useBugReport";

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

type Shot = { blob: Blob; preview: string; auto: boolean };

const drawerOpen = ref(false);
const type = ref("bug");
const title = ref("");
const description = ref("");
const steps = ref("");
const shots = ref<Shot[]>([]);
const busy = ref(false);
const capturing = ref(false);
const isDrag = ref(false);
const error = ref<string | null>(null);
const message = ref<string | null>(null);
const stats = ref({ console: 0, network: 0, networkErrors: 0, js: 0 });

const filePick = ref<HTMLInputElement | null>(null);

const reportApi = useBugReport();

const section = computed(() => detectSection(route.path));
const currentUrl = ref("");
const hasAutoShot = computed(() => shots.value.some((s) => s.auto));

const reset = () => {
  type.value = "bug";
  title.value = "";
  description.value = "";
  steps.value = "";
  shots.value.forEach((s) => URL.revokeObjectURL(s.preview));
  shots.value = [];
  error.value = null;
  message.value = null;
};

// Снимок делаем ДО открытия формы: иначе html2canvas снимет drawer и
// затемнение, и в репорт уйдёт «не та» страница.
const open = async () => {
  if (capturing.value || drawerOpen.value) return;
  reset();
  capturing.value = true;
  try {
    const blob = await reportApi.captureScreenshot();
    if (blob) shots.value.push({ blob, preview: URL.createObjectURL(blob), auto: true });
  } finally {
    capturing.value = false;
  }
  currentUrl.value = window.location.href;
  stats.value = reportApi.capturedStats();
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
  shots.value.push({ blob, preview: URL.createObjectURL(blob), auto: false });
};

const onPick = (e: Event) => {
  const target = e.target as HTMLInputElement;
  if (!target.files) return;
  Array.from(target.files).forEach(addBlob);
  target.value = "";
};
const onDrop = (e: DragEvent) => {
  isDrag.value = false;
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
.hint { color: var(--text-muted); text-transform: none; letter-spacing: 0; font-weight: var(--fw-regular); }
.input, .select, .textarea {
  width: 100%; padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-app);
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
.dropzone.is-drag { border-color: var(--accent); color: var(--accent); }
.file-hidden { display: none; }
.shots-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-3); }
.shot {
  position: relative;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  overflow: hidden;
  aspect-ratio: 1;
  background: var(--bg-surface-2);
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

.shot-auto {
  position: absolute; left: 2px; bottom: 2px;
  font-size: var(--fs-2xs); padding: 0 4px;
  border-radius: var(--rd-2);
  background: var(--accent); color: var(--accent-fg);
}
.bug-meta { margin-bottom: var(--sp-4); font-size: var(--fs-sm); color: var(--text-secondary); }
.bug-meta summary { cursor: pointer; color: var(--text-muted); }
.bug-meta ul { margin: var(--sp-2) 0 0; padding-left: var(--sp-5); }
.bug-meta code { font-family: var(--font-mono); font-size: var(--fs-xs); word-break: break-all; }
.error { color: var(--neg-strong); background: var(--neg-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.success { color: var(--pos-strong); background: var(--pos-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
