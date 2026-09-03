-- Откат 0051: настройка модели снова только через окружение.
--
-- После откатa подключение берётся исключительно из COST_LLM_* в .env, то есть
-- на проде смена модели опять требует правки CI-переменной ENV_FILE_COST и
-- деплоя стека. Настройки, введённые через админку, теряются — если разбор был
-- включён только через интерфейс, он выключится (llm_enabled() не найдёт ключа
-- в окружении).

DROP TABLE IF EXISTS cost_llm_settings;

UPDATE cost_roles
   SET permissions = permissions - 'cost:llm_admin'
 WHERE permissions @> '["cost:llm_admin"]'::jsonb;
