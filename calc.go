package main

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
)

var mathReplacer = strings.NewReplacer("×", "*", "÷", "/", "−", "-", "（", "(", "）", ")", "，", "", ",", "", " ", "")

// looksLikeMath reports whether s is worth evaluating: digits and at least
// one operator, and nothing but what arithmetic is written with.
func looksLikeMath(s string) bool {
	s = mathReplacer.Replace(strings.TrimSpace(s))
	digit, op := false, false
	for i, r := range s {
		switch {
		case unicode.IsDigit(r) || r == '.':
			digit = true
		case strings.ContainsRune("+*/%^", r), r == '-' && i > 0:
			op = true
		case strings.ContainsRune("()-", r):
		default:
			return false
		}
	}
	return digit && op
}

// evaluate computes + - * / % ^ and parentheses, with the usual precedence;
// ^ binds tightest and to the right, and % is the remainder.
func evaluate(s string) (float64, error) {
	p := &parser{s: mathReplacer.Replace(s)}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.i != len(p.s) {
		return 0, errors.New("unexpected " + string(p.s[p.i]))
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("not a number")
	}
	return v, nil
}

type parser struct {
	s string
	i int
}

func (p *parser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *parser) expr() (float64, error) {
	v, err := p.term()
	for err == nil {
		switch p.peek() {
		case '+':
			p.i++
			var r float64
			r, err = p.term()
			v += r
		case '-':
			p.i++
			var r float64
			r, err = p.term()
			v -= r
		default:
			return v, nil
		}
	}
	return 0, err
}

func (p *parser) term() (float64, error) {
	v, err := p.power()
	for err == nil {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			return v, nil
		}
		p.i++
		var r float64
		if r, err = p.power(); err != nil {
			break
		}
		switch op {
		case '*':
			v *= r
		case '/':
			if r == 0 {
				return 0, errors.New("division by zero")
			}
			v /= r
		case '%':
			if r == 0 {
				return 0, errors.New("division by zero")
			}
			v = math.Mod(v, r)
		}
	}
	return 0, err
}

func (p *parser) power() (float64, error) {
	b, err := p.unary()
	if err != nil || p.peek() != '^' {
		return b, err
	}
	p.i++
	e, err := p.power()
	return math.Pow(b, e), err
}

func (p *parser) unary() (float64, error) {
	switch p.peek() {
	case '-':
		p.i++
		v, err := p.unary()
		return -v, err
	case '+':
		p.i++
		return p.unary()
	case '(':
		p.i++
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, errors.New("missing )")
		}
		p.i++
		return v, nil
	}
	start := p.i
	for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
		p.i++
	}
	if start == p.i {
		return 0, errors.New("expected a number")
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}

// formatNumber shows a result without float noise: 0.1+0.2 is 0.3.
func formatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 10, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "0" || s == "-0" {
		return strconv.FormatFloat(v, 'g', 6, 64)
	}
	return s
}

// prettyExpr writes the expression as people read it: (1280 − 96) × 0.8.
func prettyExpr(s string) string {
	s = mathReplacer.Replace(s)
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '*':
			b.WriteString(" × ")
		case r == '/':
			b.WriteString(" ÷ ")
		case r == '+', r == '%', r == '^':
			b.WriteString(" " + string(r) + " ")
		case r == '-' && i > 0 && (unicode.IsDigit(rune(s[i-1])) || s[i-1] == ')' || s[i-1] == '.'):
			b.WriteString(" − ")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
