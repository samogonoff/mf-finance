UPDATE cost_roles
   SET permissions = permissions - 'cost:plan_prices_view',
       updated_at = now()
 WHERE name IN ('Бренд-менеджер', 'Бренд-менеджер ТЕКС');
