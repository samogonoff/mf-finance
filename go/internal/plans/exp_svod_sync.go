package plans

import (
	"fmt"
	"sync"
	"time"
)

// Загрузка из источника перед сводом расходов.
//
// Строки и факт карточки попадают в БД только при загрузке из источника, которую раньше запускало лишь открытие самой
// формы. Свод читает БД, поэтому за период, форму которого никто не открывал (например, август), он был пуст.
// Теперь свод сам запускает загрузку для открытых карточек (та же, что на форме: одна на экземпляр, с TTL и паузой после
// ошибки), ждёт её ограниченное время и честно говорит, если она ещё идёт или не удалась.

// svodSyncWait — сколько свод ждёт загрузки карточек. Из ClickHouse это секунды; прямое чтение процедуры занимает
// минуты, и тогда свод отвечает сразу с признаком «идёт загрузка», а страница опрашивает его повторно.
const svodSyncWait = 12 * time.Second

// svodLoad — карточка, которую нужно подготовить к своду; Label — как её назвать в пояснениях (страна, канал).
type svodLoad struct {
	Label string
	Card  Card
	Inst  ExpInstance
}

// loadForSvod запускает загрузку из источника для открытых карточек (параллельно) и ждёт её не дольше svodWait.
// Возвращает: идёт ли ещё загрузка и пояснения по тем карточкам, где последняя загрузка не удалась.
// Закрытые (утверждённые, на согласовании) карточки не загружаются: утверждённая форма остаётся такой, как согласована.
func (s *ExpService) loadForSvod(items []svodLoad) (syncing bool, warnings []string) {
	if s.source == nil || len(items) == 0 {
		return false, nil
	}
	var wg sync.WaitGroup
	for _, it := range items {
		if !cardEditable(it.Card) {
			continue
		}
		wg.Add(1)
		go func(it svodLoad) {
			defer wg.Done()
			s.ensureSync(it.Card, it.Inst, false)
		}(it)
	}
	wg.Wait()

	wait := s.svodWait
	if wait <= 0 {
		wait = svodSyncWait
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) && s.anySyncRunning(items) {
		time.Sleep(150 * time.Millisecond)
	}
	for _, it := range items {
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

func (s *ExpService) anySyncRunning(items []svodLoad) bool {
	for _, it := range items {
		if s.syncStatus(it.Inst.ID).State == ExpSyncRunning {
			return true
		}
	}
	return false
}

// svodEmptyClosedWarning — пояснение для закрытой карточки без строк: загрузка для неё не выполняется, поэтому пустой
// свод нельзя оставлять без объяснения.
func svodEmptyClosedWarning(label string) string {
	return fmt.Sprintf("%s: карточка закрыта, строк расходов в ней нет (для закрытых карточек загрузка из источника не выполняется).", label)
}
