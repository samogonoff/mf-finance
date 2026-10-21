package plans

import (
	"context"
	"fmt"
	"time"
)

// Загрузка из источника перед сводом расходов.
//
// Строки и факт карточки попадают в БД только при загрузке из источника, которую раньше запускало лишь открытие самой
// формы. Свод читает БД, поэтому за период, форму которого никто не открывал (например, август), он был пуст.
// Теперь свод сам запускает загрузку для открытых карточек (та же, что на форме: одна на экземпляр, с TTL и паузой после
// ошибки), ждёт её ограниченное время и честно говорит, если она ещё идёт или не удалась.

// svodSyncWait — сколько свод ждёт загрузки карточек. Из ClickHouse это секунды; прямое чтение процедуры занимает
// минуты, и тогда свод отвечает с признаком «идёт загрузка», а страница опрашивает его повторно. Ответ укладывается в
// WriteTimeout сервера (30 с): ожидание одно на запрос, а не на канал.
const svodSyncWait = 12 * time.Second

// svodLoad — карточка, которую нужно подготовить к своду; Label — как её назвать в пояснениях (страна, канал).
type svodLoad struct {
	Label string
	Card  Card
	Inst  ExpInstance
}

type svodDeadlineKey struct{}

// withSvodDeadline кладёт в контекст общий дедлайн ожидания загрузок. Общий свод читает три канала параллельно и делит
// один дедлайн, а не складывает ожидания (иначе на медленном источнике ответ не уложился бы в WriteTimeout).
func withSvodDeadline(ctx context.Context, wait time.Duration) context.Context {
	return context.WithValue(ctx, svodDeadlineKey{}, time.Now().Add(wait))
}

func svodDeadline(ctx context.Context, wait time.Duration) time.Time {
	if d, ok := ctx.Value(svodDeadlineKey{}).(time.Time); ok {
		return d
	}
	return time.Now().Add(wait)
}

// svodWaitOrDefault — ожидание загрузок своду (0 в тестах — по умолчанию).
func (s *ExpService) svodWaitOrDefault() time.Duration {
	if s.svodWait > 0 {
		return s.svodWait
	}
	return svodSyncWait
}

// loadForSvod запускает загрузку из источника для открытых карточек (параллельно, без ожидания внутри) и ждёт все
// запущенные один раз — до общего дедлайна или отмены запроса. Возвращает: идёт ли ещё загрузка и пояснения по
// карточкам, где последняя загрузка не удалась. Закрытые (утверждённые, на согласовании) карточки не загружаются
// и в статусах не участвуют: утверждённая форма остаётся такой, как согласована.
func (s *ExpService) loadForSvod(ctx context.Context, items []svodLoad) (syncing bool, warnings []string) {
	if s.source == nil || len(items) == 0 {
		return false, nil
	}
	var open []svodLoad
	var runs []*expSyncRun
	for _, it := range items {
		if !cardEditable(it.Card) {
			continue
		}
		open = append(open, it)
		if run := s.startSync(it.Card, it.Inst, false); run != nil {
			runs = append(runs, run)
		}
	}
	waitRuns(ctx, runs, svodDeadline(ctx, s.svodWaitOrDefault()))
	for _, it := range open {
		st := s.syncStatus(it.Inst.ID)
		switch st.State {
		case ExpSyncRunning:
			syncing = true
		case ExpSyncError:
			warnings = append(warnings, fmt.Sprintf("%s: загрузка из источника не удалась — %s. Показано то, что уже загружено.", it.Label, st.Error))
		}
	}
	return syncing, warnings
}

// waitRuns ждёт окончания загрузок не дольше дедлайна; прерывается отменой запроса (клиент ушёл — горутина не висит).
func waitRuns(ctx context.Context, runs []*expSyncRun, deadline time.Time) {
	if len(runs) == 0 {
		return
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for _, run := range runs {
		select {
		case <-run.done:
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// svodEmptyClosedWarning — пояснение для закрытой карточки без строк: загрузка для неё не выполняется, поэтому пустой
// свод нельзя оставлять без объяснения.
func svodEmptyClosedWarning(label string) string {
	return fmt.Sprintf("%s: карточка закрыта, строк расходов в ней нет (для закрытых карточек загрузка из источника не выполняется).", label)
}
