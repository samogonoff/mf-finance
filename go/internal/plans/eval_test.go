package plans

import (
	"math"
	"testing"
)

func evalOK(t *testing.T, expr string, vars map[string]float64, want float64) {
	t.Helper()
	got, err := Eval(expr, vars)
	if err != nil {
		t.Fatalf("Eval(%q) error: %v", expr, err)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Eval(%q) = %v, want %v", expr, got, want)
	}
}

func TestEval_Arithmetic(t *testing.T) {
	evalOK(t, "2 + 3 * 4", nil, 14)
	evalOK(t, "(2 + 3) * 4", nil, 20)
	evalOK(t, "10 / 4", nil, 2.5)
	evalOK(t, "-5 + 2", nil, -3)
	evalOK(t, "2 * -3", nil, -6)
}

func TestEval_Variables(t *testing.T) {
	vars := map[string]float64{"sales": 1000, "cost": 600}
	evalOK(t, "sales - cost", vars, 400)
	evalOK(t, "(sales - cost) / cost", vars, 400.0/600.0)
	evalOK(t, "sales / (1 + vat)", map[string]float64{"sales": 120, "vat": 0.2}, 100)
}

func TestEval_Functions(t *testing.T) {
	evalOK(t, "MAX(3, 7)", nil, 7)
	evalOK(t, "MIN(3, 7)", nil, 3)
	evalOK(t, "ABS(-5)", nil, 5)
	evalOK(t, "MAX(sales * 0.1, 50)", map[string]float64{"sales": 1000}, 100)
}

func TestEval_DivByZero(t *testing.T) {
	if _, err := Eval("1 / 0", nil); err == nil {
		t.Error("деление на ноль должно быть ошибкой")
	}
	if _, err := Eval("x / cost", map[string]float64{"x": 1, "cost": 0}); err == nil {
		t.Error("деление на нулевую переменную должно быть ошибкой")
	}
}

func TestEval_UnknownVariable(t *testing.T) {
	if _, err := Eval("a + b", map[string]float64{"a": 1}); err == nil {
		t.Error("неизвестная переменная должна быть ошибкой")
	}
}

func TestEval_UnknownFunction(t *testing.T) {
	if _, err := Eval("FOO(1)", nil); err == nil {
		t.Error("неизвестная функция должна быть ошибкой")
	}
}

func TestEval_SyntaxError(t *testing.T) {
	for _, expr := range []string{"2 +", "(1 + 2", "* 3", "1 2"} {
		if _, err := Eval(expr, nil); err == nil {
			t.Errorf("ожидалась синтаксическая ошибка для %q", expr)
		}
	}
}
