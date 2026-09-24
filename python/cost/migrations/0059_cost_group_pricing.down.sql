-- Откат 0059: ценообразование группы — компании, цепочки поставки, звенья,
-- витрина для 1С и право cost:group_pricing.
-- Цепочки и справочник компаний ТЕРЯЮТСЯ — перед откатом на проде выгрузить
-- cost_group_pricing_export (или три таблицы), если данные ещё нужны.

DROP VIEW IF EXISTS cost_group_pricing_export;

DROP TABLE IF EXISTS cost_group_chain_link;
DROP TABLE IF EXISTS cost_group_chain;
DROP TABLE IF EXISTS cost_group_company;

UPDATE cost_roles
   SET permissions = permissions - 'cost:group_pricing',
       updated_at = now()
 WHERE permissions @> '["cost:group_pricing"]'::jsonb;
