package plans

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Песочница приёмника публикации (миграция 0041).
//
// Зачем отдельная реализация Publisher, а не «mock»: проверять надо не формулы, а
// САМ ПУТЬ ЗАПИСИ — транзакцию DELETE+INSERT по логическому ключу, обратное
// чтение суммы из приёмника, контроль «Σ введено − Σ записано = 0»,
// идемпотентность по (карточка, версия, приёмник) и перевод карточки в
// published/publish_failed. Всё это в mock'е не воспроизводится, а с Budgeting
// в dev'е соединения нет. Поэтому приёмник изображает таблица publish_sandbox в
// БД finance, а алгоритм остаётся ровно тот же, что у mssqlPublisher.
//
// Отличия от прода — только те, что диктует СУБД:
//   - параметры $1..$9 вместо @p1..@p9;
//   - имя таблицы приёмника не подставляется в SQL (пишем всегда в
//     publish_sandbox), но белый список PLANS_PUBLISH_TARGETS проверяется тем же
//     правилом: в песочнице должно быть невозможно то, что невозможно в проде.
//
// Специально НЕ отличается: отсутствие уникального индекса (в приёмниках Budgeting
// его тоже нет) и построчный DELETE+INSERT — если пачка содержит два значения на
// один логический ключ, песочница обязана дать ту же потерю, что и приёмник.
type sandboxPublisher struct {
	pool    *pgxpool.Pool
	allowed map[string]bool // тот же белый список таблиц-приёмников, что у прода
}

// NewSandboxPublisher — приёмник-заменитель в PG. Включается PLANS_PUBLISH_SANDBOX=1
// (только dev/тест: карточка получит статус published, а в Budgeting записи не будет).
func NewSandboxPublisher(pool *pgxpool.Pool, targets []string) Publisher {
	allowed := map[string]bool{}
	for _, t := range targets {
		if t = strings.TrimSpace(t); t != "" {
			allowed[strings.ToLower(t)] = true
		}
	}
	// Приёмники расходов разрешены в песочнице всегда: реальная запись в них невозможна (см. publishSandboxOnlyTargets).
	for _, t := range publishSandboxOnlyTargets {
		allowed[strings.ToLower(t)] = true
	}
	return &sandboxPublisher{pool: pool, allowed: allowed}
}

func (p *sandboxPublisher) Available() bool { return p != nil && p.pool != nil }
func (p *sandboxPublisher) Sandbox() bool   { return true }

// checkTarget — та же защита, что в проде: имя приёмника приходит из данных
// publish_mapping, поэтому вне белого списка запись невозможна и в песочнице.
func (p *sandboxPublisher) checkTarget(target string) error {
	if !p.allowed[strings.ToLower(strings.TrimSpace(target))] {
		return fmt.Errorf("таблица-приёмник %q не разрешена конфигом (PLANS_PUBLISH_TARGETS)", target)
	}
	return nil
}

func (p *sandboxPublisher) Probe(ctx context.Context, target string, rows []PublishRow) (float64, error) {
	if err := p.checkTarget(target); err != nil {
		return 0, err
	}
	var sum float64
	for _, r := range rows {
		var v *float64
		err := p.pool.QueryRow(ctx, `
			SELECT SUM(value) FROM publish_sandbox
			 WHERE target_table = $1 AND param = $2 AND country = $3
			   AND code_cfo = $4 AND code_pl = $5 AND plan_date = $6 AND company = $7`,
			target, r.Param, r.Country, fmt.Sprint(r.CodeCFO), fmt.Sprint(r.CodePL), r.Date, r.Company).Scan(&v)
		if err != nil {
			return 0, err
		}
		if v != nil {
			sum += *v
		}
	}
	return round4(sum), nil
}

func (p *sandboxPublisher) Write(ctx context.Context, target string, rows []PublishRow) (int, float64, error) {
	if err := p.checkTarget(target); err != nil {
		return 0, 0, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	written := 0
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `
			DELETE FROM publish_sandbox
			WHERE target_table = $1 AND param = $2 AND country = $3
			   AND code_cfo = $4 AND code_pl = $5 AND plan_date = $6 AND company = $7`,
			target, r.Param, r.Country, fmt.Sprint(r.CodeCFO), fmt.Sprint(r.CodePL), r.Date, r.Company); err != nil {
			return written, 0, fmt.Errorf("очистка ключа (%s/%d/%s): %w",
				r.Param, r.CodeCFO, r.Date.Format("2006-01-02"), err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO publish_sandbox (target_table, param, country, code_cfo,
				group_cfo1, group_cfo2, cfo_name, code_pl, plan_date, value, company)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			target, r.Param, r.Country, fmt.Sprint(r.CodeCFO), r.GroupCFO1, r.GroupCFO2,
			r.CFOName, fmt.Sprint(r.CodePL), r.Date, r.Value, r.Company); err != nil {
			return written, 0, fmt.Errorf("запись строки (%s/%d/%s): %w",
				r.Param, r.CodeCFO, r.Date.Format("2006-01-02"), err)
		}
		written++
	}
	if err := tx.Commit(ctx); err != nil {
		return written, 0, err
	}
	// Сумму читаем ПОСЛЕ коммита и из таблицы — как в проде: контроль записи
	// должен ловить расхождение, а не подтверждать сам себя из памяти.
	sum, err := p.Probe(ctx, target, rows)
	return written, sum, err
}

// Rows — содержимое песочницы (доказательство записи: смотрим то, что реально
// легло в «приёмник», а не то, что собирались отправить). Реализует SandboxReader.
func (p *sandboxPublisher) Rows(ctx context.Context, target string, limit int) ([]PublishRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if target != "" {
		if err := p.checkTarget(target); err != nil {
			return nil, err
		}
	}
	q := `SELECT target_table, param, country, code_cfo, group_cfo1, group_cfo2,
	             cfo_name, code_pl, plan_date, value, company
	        FROM publish_sandbox
	       WHERE ($1 = '' OR target_table = $1)
	       ORDER BY written_at DESC, id DESC LIMIT $2`
	rows, err := p.pool.Query(ctx, q, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublishRow{}
	for rows.Next() {
		var r PublishRow
		var codeCFO, codePL string
		if err := rows.Scan(&r.TargetTable, &r.Param, &r.Country, &codeCFO, &r.GroupCFO1,
			&r.GroupCFO2, &r.CFOName, &codePL, &r.Date, &r.Value, &r.Company); err != nil {
			return nil, err
		}
		// КодЦФО/КодPL в приёмнике — строки; наружу отдаём числами, как в форме.
		r.CodeCFO, _ = strconv.Atoi(strings.TrimSpace(codeCFO))
		r.CodePL, _ = strconv.Atoi(strings.TrimSpace(codePL))
		out = append(out, r)
	}
	return out, rows.Err()
}
