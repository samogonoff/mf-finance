UPDATE exp_row
   SET attrs = attrs - 'retail_cfo40_reassigned', updated_at = NOW()
 WHERE attrs ? 'retail_cfo40_reassigned';
