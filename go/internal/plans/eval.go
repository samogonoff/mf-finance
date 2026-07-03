package plans

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
)

// Безопасный вычислитель арифметических выражений для движка CALC (D11).
// Поддержка: числа, переменные (из map), + − × ÷, скобки, унарный минус,
// функции MAX/MIN/ABS. НЕ eval произвольного кода — только whitelisted грамматика.
// Используется для формул calc_rule / pl_formula_override (SPEC §11).

// Eval вычисляет выражение expr со значениями переменных vars.
func Eval(expr string, vars map[string]float64) (float64, error) {
	p := &evalParser{src: expr, vars: vars}
	p.next()
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.tok.kind != tokEOF {
		return 0, fmt.Errorf("лишний токен %q", p.tok.text)
	}
	return v, nil
}

type tokKind int

const (
	tokEOF tokKind = iota
	tokNum
	tokIdent
	tokOp
	tokLParen
	tokRParen
	tokComma
)

type token struct {
	kind tokKind
	text string
	num  float64
}

type evalParser struct {
	src  string
	pos  int
	tok  token
	vars map[string]float64
}

func (p *evalParser) next() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos >= len(p.src) {
		p.tok = token{kind: tokEOF}
		return
	}
	c := p.src[p.pos]
	switch {
	case c == '(':
		p.tok = token{kind: tokLParen, text: "("}
		p.pos++
	case c == ')':
		p.tok = token{kind: tokRParen, text: ")"}
		p.pos++
	case c == ',':
		p.tok = token{kind: tokComma, text: ","}
		p.pos++
	case strings.ContainsRune("+-*/", rune(c)):
		p.tok = token{kind: tokOp, text: string(c)}
		p.pos++
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		var f float64
		_, err := fmt.Sscanf(p.src[start:p.pos], "%g", &f)
		if err != nil {
			p.tok = token{kind: tokEOF}
			return
		}
		p.tok = token{kind: tokNum, num: f, text: p.src[start:p.pos]}
	case unicode.IsLetter(rune(c)) || c == '_':
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsLetter(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos])) || p.src[p.pos] == '_') {
			p.pos++
		}
		p.tok = token{kind: tokIdent, text: p.src[start:p.pos]}
	default:
		p.tok = token{kind: tokEOF, text: string(c)}
	}
}

// expr := term (('+'|'-') term)*
func (p *evalParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "+" || p.tok.text == "-") {
		op := p.tok.text
		p.next()
		r, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

// term := factor (('*'|'/') factor)*
func (p *evalParser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.tok.kind == tokOp && (p.tok.text == "*" || p.tok.text == "/") {
		op := p.tok.text
		p.next()
		r, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == "*" {
			v *= r
		} else {
			if r == 0 {
				return 0, errors.New("деление на ноль")
			}
			v /= r
		}
	}
	return v, nil
}

// factor := '-' factor | number | ident ['(' args ')'] | '(' expr ')'
func (p *evalParser) parseFactor() (float64, error) {
	switch {
	case p.tok.kind == tokOp && p.tok.text == "-":
		p.next()
		v, err := p.parseFactor()
		return -v, err
	case p.tok.kind == tokOp && p.tok.text == "+":
		p.next()
		return p.parseFactor()
	case p.tok.kind == tokNum:
		v := p.tok.num
		p.next()
		return v, nil
	case p.tok.kind == tokLParen:
		p.next()
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.tok.kind != tokRParen {
			return 0, errors.New("ожидалась ')'")
		}
		p.next()
		return v, nil
	case p.tok.kind == tokIdent:
		name := p.tok.text
		p.next()
		if p.tok.kind == tokLParen {
			return p.parseCall(name)
		}
		v, ok := p.vars[name]
		if !ok {
			return 0, fmt.Errorf("неизвестная переменная %q", name)
		}
		return v, nil
	default:
		return 0, fmt.Errorf("неожиданный токен %q", p.tok.text)
	}
}

func (p *evalParser) parseCall(name string) (float64, error) {
	p.next() // съели '('
	args := []float64{}
	if p.tok.kind != tokRParen {
		for {
			a, err := p.parseExpr()
			if err != nil {
				return 0, err
			}
			args = append(args, a)
			if p.tok.kind == tokComma {
				p.next()
				continue
			}
			break
		}
	}
	if p.tok.kind != tokRParen {
		return 0, errors.New("ожидалась ')' в вызове функции")
	}
	p.next()
	switch strings.ToUpper(name) {
	case "MAX":
		if len(args) == 0 {
			return 0, errors.New("MAX без аргументов")
		}
		m := args[0]
		for _, a := range args[1:] {
			m = math.Max(m, a)
		}
		return m, nil
	case "MIN":
		if len(args) == 0 {
			return 0, errors.New("MIN без аргументов")
		}
		m := args[0]
		for _, a := range args[1:] {
			m = math.Min(m, a)
		}
		return m, nil
	case "ABS":
		if len(args) != 1 {
			return 0, errors.New("ABS требует 1 аргумент")
		}
		return math.Abs(args[0]), nil
	default:
		return 0, fmt.Errorf("неизвестная функция %q", name)
	}
}
