-- 0058 down — забрать cost:insights у калькулятора и бренд-менеджера (как в 0045).
UPDATE cost_roles
   SET permissions = permissions - 'cost:insights',
       updated_at = now()
 WHERE name IN ('Калькулятор', 'Бренд-менеджер');
