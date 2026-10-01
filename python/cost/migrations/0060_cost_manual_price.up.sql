-- 0060 — ручные опт и розница у ПФКСС без привязки к уровню цен (задача Б24
-- 661209, решение заказчика 01.10.2026).
--
-- При расценке товара для маркетплейсов розница не обязана совпадать ни с одним
-- уровнем справочника s_price_level. Отдельная роль ставит опт и розницу руками;
-- наценка либо выбирается из вариантов уровня, как у бренд-менеджера, либо
-- пересчитывается от введённого опта. Калькуляции — этапа ПФКСС, в Лису уходят
-- при согласовании ПЭО: если пара опт+розница не совпала с уровнем, в процедуру
-- create_priceList_inFox идёт price_level_id = 0, и она создаёт прейскурант,
-- не трогая s_modeli.PRICE_LEVEL_ID (как у КПСС).

-- Признак ручной цены в заявке: ПЭО видит его в окне согласования. В DWH и в
-- процедуру он не передаётся — там решает пустой уровень.
ALTER TABLE cost_price_pending
    ADD COLUMN IF NOT EXISTS price_manual BOOLEAN NOT NULL DEFAULT FALSE;

-- Роль «Бренд-менеджер ТЕКС» — права бренд-менеджера плюс cost:manual_price.
-- Права копируются с роли «Бренд-менеджер» на момент наката: на проде набор
-- у неё расширялся миграциями (0052, 0053, 0058) и мог правиться из админки.
INSERT INTO cost_roles (name, permissions, is_system)
SELECT 'Бренд-менеджер ТЕКС',
       COALESCE(
           (SELECT permissions FROM cost_roles WHERE name = 'Бренд-менеджер'),
           '["cost:view", "cost:edit_price", "cost:export"]'::jsonb
       ) || '["cost:manual_price"]'::jsonb,
       TRUE
ON CONFLICT (name) DO UPDATE
   SET permissions = cost_roles.permissions || '["cost:manual_price"]'::jsonb,
       updated_at  = now()
 WHERE NOT cost_roles.permissions @> '["cost:manual_price"]'::jsonb;

UPDATE cost_roles
   SET permissions = permissions || '["cost:manual_price"]'::jsonb,
       updated_at = now()
 WHERE name = 'Full Admin'
   AND NOT permissions @> '["cost:manual_price"]'::jsonb;
