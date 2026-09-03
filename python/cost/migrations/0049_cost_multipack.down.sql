DROP TABLE IF EXISTS cost_multipack_build;
DROP TABLE IF EXISTS cost_multipack_item;
DROP TABLE IF EXISTS cost_multipack;

UPDATE cost_roles
   SET permissions = permissions - 'cost:multipack',
       updated_at = now()
 WHERE permissions @> '["cost:multipack"]'::jsonb;
