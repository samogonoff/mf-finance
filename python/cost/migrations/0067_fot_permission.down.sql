-- Откат 0067: снять право cost:fot.
UPDATE cost_roles
   SET permissions = permissions - 'cost:fot'
 WHERE permissions @> '["cost:fot"]'::jsonb;
