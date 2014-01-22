package main

import (
	"bufio"
	"bytes"
	"fmt"
	// "go/ast"
	"go/printer"
	goToken "go/token"
	"io/ioutil"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Any interface{}
type Symbol string
type tokenType int

type Pos int

type item struct {
	typ itemType
	pos Pos
	val string
}

type itemType int

const (
	itemError itemType = iota
	itemEOF

	itemLeftParen
	itemRightParen
	itemLeftVect
	itemRightVect

	itemIdent
	itemString
	itemChar
	itemFloat
	itemInt

	itemQuote
	itemQuasiQuote
	itemUnquote
	itemUnquoteSplice
)

const eof = -1

type stateFn func(*lexer) stateFn

type lexer struct {
	name    string
	input   string
	state   stateFn
	pos     Pos
	start   Pos
	width   Pos
	lastPos Pos
	items   chan item

	parenDepth int
	vectDepth  int
}

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if int(l.pos) >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = Pos(w)
	l.pos += l.width
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// backup steps back one rune. Can only be called once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.tokens <- item{t, l.start, l.input[l.start:l.pos]}
	l.start = l.pos
}

func (l *lexer) ignore() {
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid string) bool {
	if strings.IndexRune(valid, l.next()) >= 0 {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes a run of runes from the valid set.
func (l *lexer) acceptRun(valid string) {
	for strings.IndexRune(valid, l.next()) >= 0 {
	}
	l.backup()
}

func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.tokens <- item{itemError, l.start, fmt.Sprintf(format, args...)}
	return nil
}

func (l *lexer) nextItem() token {
	item := <-l.items
	l.lastPos = item.pos
	return item
}

func lex(name, input string) *lexer {
	l := &lexer{
		name:       name,
		input:  input,
		items: make(chan item),
	}
	go l.run()
	return l
}

func (l *lexer) run() {
	for l.state = lexWhitespace; l.state != nil; {
		l.state = l.state(l)
	}
	close(l.items)
}

func lexOpenVect(l *lexer) stateFn {
	l.emit(_LVECT)
	l.vectDepth++

	r := l.next()

	switch r {
	case ' ', '\t', '\n', '\r':
		return lexWhitespace
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case ';':
		return lexComment
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol

}

func lexCloseVect(l *lexer) stateFn {
	l.emit(_RVECT)
	l.vectDepth--
	if l.parenDepth < 0 {
		return l.errorf("unexpected close paren [vect]")
	}

	r := l.next()

	switch r {
	case ' ', '\t', '\n', '\r':
		return lexWhitespace
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case ';':
		return lexComment
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

// lexes an open parenthesis
func lexOpenParen(l *lexer) stateFn {

	l.emit(_LPAREN)
	l.parenDepth++

	r := l.next()

	switch r {
	case ' ', '\t', '\n', '\r':
		return lexWhitespace
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case ';':
		return lexComment
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexQuote(l *lexer) stateFn {
	l.acceptRun(" ")
	l.ignore()
	l.emit(_QUOTE)

	r := l.next()

	switch r {
	case '"':
		return lexString
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexQuasiquote(l *lexer) stateFn {
	l.acceptRun(" ")
	l.ignore()
	l.emit(_QUASIQUOTE)

	r := l.next()

	switch r {
	case '"':
		return lexString
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexUnquote(l *lexer) stateFn {

	if l.peek() == '@' {
		return lexUnquoteSplice
	}

	l.acceptRun(" ")
	l.ignore()
	l.emit(_UNQUOTE)

	r := l.next()

	switch r {
	case '"':
		return lexString
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexUnquoteSplice(l *lexer) stateFn {
	r := l.next()
	l.acceptRun(" ")
	l.ignore()
	l.emit(_UNQUOTESPLICE)

	r = l.next()

	switch r {
	case '"':
		return lexString
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexWhitespace(l *lexer) stateFn {
	l.ignore()
	r := l.next()

	switch r {
	case ' ', '\t', '\n':
		return lexWhitespace
	case '\'':
		return lexQuote
	case '`':
		return lexQuasiquote
	case ',':
		return lexUnquote
	case '"':
		return lexString
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case ';':
		return lexComment
	case eof:
		if l.parenDepth > 0 {
			return l.errorf("unclosed paren")
		}
		l.emit(_EOF)
		return nil
	}

	if unicode.IsDigit(r) {
		return lexInt
	}

	return lexSymbol
}

func lexString(l *lexer) stateFn {
	r := l.next()

	switch r {
	case '"':
		l.emit(_STRING)
		return lexWhitespace
	case '\\':
		// l.backup()
		// l.input = append(l.input[:l.pos], l.input[l.pos+1:])
		l.next()
		return lexString
	}

	return lexString
}

func lexInt(l *lexer) stateFn {
	digits := "0123456789"
	l.acceptRun(digits)

	r := l.peek()

	switch r {
	case ' ', '\t', '\n':
		l.emit(_INT)
		l.next()
		return lexWhitespace
	case '.':
		l.next()
		return lexFloat
	case ')':
		l.emit(_INT)
		l.next()
		return lexCloseParen
	case ';':
		l.emit(_INT)
		l.next()
		return lexComment
	}

	return l.errorf("unexpected rune in lexInt: %c", r)
}

// once we're in a float, the only valid values are digits, whitespace or close
// paren.
func lexFloat(l *lexer) stateFn {

	digits := "0123456789"
	l.acceptRun(digits)

	l.emit(_FLOAT)

	r := l.next()

	switch r {
	case ' ', '\t', '\n':
		return lexWhitespace
	case ')':
		return lexCloseParen
	case ';':
		return lexComment
	}

	return l.errorf("unexpected run in lexFloat: %c", r)
}

func lexSymbol(l *lexer) stateFn {

	r := l.peek()

	switch r {
	case ' ', '\t', '\n':
		l.emit(_SYMBOL)
		l.next()
		return lexWhitespace
	case ')':
		l.emit(_SYMBOL)
		l.next()
		return lexCloseParen
	case ';':
		l.emit(_SYMBOL)
		l.next()
		return lexComment
	default:
		l.next()
		return lexSymbol
	}
}

// lex a close parenthesis
func lexCloseParen(l *lexer) stateFn {
	l.emit(_RPAREN)
	l.parenDepth--
	if l.parenDepth < 0 {
		return l.errorf("unexpected close paren")
	}

	r := l.next()
	switch r {
	case ' ', '\t', '\n':
		return lexWhitespace
	case '(':
		return lexOpenParen
	case ')':
		return lexCloseParen
	case '[':
		return lexOpenVect
	case ']':
		return lexCloseVect
	case ';':
		return lexComment
	}
	return l.errorf("unimplemented")
}

// lexes a comment
func lexComment(l *lexer) stateFn {

	r := l.next()

	switch r {
	case '\n', '\r':
		return lexWhitespace
	}
	return lexComment
}

func parse(l *lexer, p []Any) []Any {

	for {
		t := l.nextToken()
		if t.typ == _EOF {
			break
		} else if t.typ == _INVALID {
			panic("syntax error")
		}

		if t.typ == _LPAREN {
			p = append(p, parse(l, []Any{}))
			return parse(l, p)
		} else if t.typ == _RPAREN {
			return p
		} else {
			var v astToken
			v.Value = t.val
			switch t.typ {
			// case _UNQUOTESPLICE:
			// 	nextExp := parse(l, []Any{})
			// 	return append(append(p, []Any{Symbol("unquote-splice"), nextExp[0]}), nextExp[1:]...)
			// case _UNQUOTE:
			// 	nextExp := parse(l, []Any{})
			// 	return append(append(p, []Any{Symbol("unquote"), nextExp[0]}), nextExp[1:]...)
			// case _QUASIQUOTE:
			// 	nextExp := parse(l, []Any{})
			// 	return append(append(p, []Any{Symbol("quasiquote"), nextExp[0]}), nextExp[1:]...)
			// case _QUOTE:
			// 	nextExp := parse(l, []Any{})
			// 	return append(append(p, []Any{Symbol("quote"), nextExp[0]}), nextExp[1:]...)
			case _INT:
				v.Type = "INT"
			case _FLOAT:
				v.Type = "FLOAT"
			case _STRING:
				v.Type = "STRING"
			case _SYMBOL:
				v.Type = "IDENT"
			}
			return parse(l, append(p, v))
		}
	}

	return p
}

func args(filename string) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	l := lex(string(b) + "\n")
	p := parse(l, []Any{})
	// fmt.Printf("%#v\n\n\n", p)

	a := generateAST(p)

	fset := goToken.NewFileSet()
	// ast.Print(fset, a)

	var buf bytes.Buffer
	printer.Fprint(&buf, fset, a)
	fmt.Printf("%s\n", buf.String())
}

func main() {
	if len(os.Args) > 1 {
		args(os.Args[1])
		return
	}

	r := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(">> ")
		line, _, _ := r.ReadLine()

		l := lex(string(line) + "\n")
		p := parse(l, []Any{})

		a := generateAST(p)
		fset := goToken.NewFileSet()
		var buf bytes.Buffer
		printer.Fprint(&buf, fset, a)
		fmt.Printf("%s\n", buf.String())
	}
}
