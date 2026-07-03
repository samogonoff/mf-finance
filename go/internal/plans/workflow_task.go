package plans

import "fmt"

// Движок ЗАДАНИЙ на этапе (надстройка над stage-машиной workflow.go). Задание =
// форма (TPL-*) × ЦФО-срез + роль (fill|approve). Исполнитель авто = ТОП этих ЦФО
// (Должность) / Директор ЮЛ для согласования. Делегирование ДВУХступенчатое:
// исполнитель делегирует → делегат делает → submit → review → исполнитель
// принимает (accept) → done. Машина чистая (тестируема без БД).

// Статусы задания.
const (
	TaskPending    = "pending"     // не начато
	TaskInProgress = "in_progress" // в работе (исполнитель или делегат)
	TaskReview     = "review"      // делегат сдал, ждёт приёмки исполнителем
	TaskDone       = "done"        // принято/выполнено
	TaskReturned   = "returned"    // возвращено на доработку
)

// TaskRole — роль задания.
const (
	RoleFill    = "fill"    // заполнение формы
	RoleApprove = "approve" // согласование
)

// applyTaskAction — переход статуса задания. delegated=true, если назначен делегат.
// Действия: start, delegate, submit, accept, return, reopen.
//   • submit без делегата → done (исполнитель сам сделал);
//   • submit с делегатом (сдаёт делегат) → review (исполнитель примет);
//   • accept (review→done) — приёмка исполнителем;
//   • return — на доработку.
func applyTaskAction(status, action string, delegated bool) (string, error) {
	switch action {
	case "start":
		if status == TaskPending || status == TaskReturned {
			return TaskInProgress, nil
		}
	case "delegate":
		// Делегирование не меняет статус резко: задание в работе у делегата.
		if status == TaskPending || status == TaskInProgress || status == TaskReturned {
			return TaskInProgress, nil
		}
	case "submit":
		if status == TaskInProgress {
			if delegated {
				return TaskReview, nil
			}
			return TaskDone, nil
		}
	case "accept":
		if status == TaskReview {
			return TaskDone, nil
		}
	case "return":
		if status == TaskReview {
			return TaskInProgress, nil
		}
		if status == TaskInProgress || status == TaskDone {
			return TaskReturned, nil
		}
	case "reopen":
		if status == TaskDone || status == TaskReturned {
			return TaskInProgress, nil
		}
	default:
		return "", fmt.Errorf("неизвестное действие задания %q", action)
	}
	return "", fmt.Errorf("действие %q недопустимо из статуса %q", action, status)
}

// taskTerminalDone — задание завершено (для проверки «все задания этапа done»).
func taskTerminalDone(status string) bool { return status == TaskDone }
