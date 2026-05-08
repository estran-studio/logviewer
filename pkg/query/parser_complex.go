package query

import (
	"fmt"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
)

// Parser parses complex query expressions into Filter AST
type Parser struct {
	tokens []Token
	pos    int
}

// NewParser creates a new parser from tokens
func NewParser(tokens []Token) *Parser {
	return &Parser{
		tokens: tokens,
		pos:    0,
	}
}

// ParseQuery parses a complete query expression.
// Grammar:
//
//	query     = or_expr
//	or_expr   = and_expr ("OR" and_expr)*
//	and_expr  = not_expr ("AND" not_expr)*
//	not_expr  = "NOT"? primary
//	primary   = "(" query ")" | exists_func | condition
//	exists_func = "exists" "(" field ")"
//	condition = field operator value
func (p *Parser) ParseQuery() (*client.Filter, error) {
	filter, err := p.parseOrExpr()
	if err != nil {
		return nil, err
	}

	// Ensure we consumed all tokens
	if p.current().Type != TokenEOF {
		return nil, fmt.Errorf("unexpected token '%s' at position %d", p.current().Value, p.current().Pos)
	}

	return filter, nil
}

// current returns the current token
func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

// advance moves to the next token
func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// expect checks if the current token matches the expected type (unused)
// func (p *Parser) expect(t TokenType) error {
// 	if p.current().Type != t {
// 		return fmt.Errorf("expected token type %d, got %d at position %d", t, p.current().Type, p.current().Pos)
// 	}
// 	return nil
// }

// parseOrExpr parses: and_expr ("OR" and_expr)*
func (p *Parser) parseOrExpr() (*client.Filter, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}

	var filters []client.Filter
	filters = append(filters, *left)

	for p.current().Type == TokenOr {
		p.advance() // consume OR

		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		filters = append(filters, *right)
	}

	if len(filters) == 1 {
		return &filters[0], nil
	}

	return &client.Filter{
		Logic:   client.LogicOr,
		Filters: filters,
	}, nil
}

// parseAndExpr parses: not_expr ("AND" not_expr)*
func (p *Parser) parseAndExpr() (*client.Filter, error) {
	left, err := p.parseNotExpr()
	if err != nil {
		return nil, err
	}

	var filters []client.Filter
	filters = append(filters, *left)

	for p.current().Type == TokenAnd {
		p.advance() // consume AND

		right, err := p.parseNotExpr()
		if err != nil {
			return nil, err
		}
		filters = append(filters, *right)
	}

	if len(filters) == 1 {
		return &filters[0], nil
	}

	return &client.Filter{
		Logic:   client.LogicAnd,
		Filters: filters,
	}, nil
}

// parseNotExpr parses: "NOT"? primary
func (p *Parser) parseNotExpr() (*client.Filter, error) {
	if p.current().Type == TokenNot {
		p.advance() // consume NOT

		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}

		return &client.Filter{
			Logic:   client.LogicNot,
			Filters: []client.Filter{*inner},
		}, nil
	}

	return p.parsePrimary()
}

// parsePrimary parses: "(" query ")" | exists_func | condition
func (p *Parser) parsePrimary() (*client.Filter, error) {
	// Check for parenthesized expression
	if p.current().Type == TokenLParen {
		p.advance() // consume (

		inner, err := p.parseOrExpr()
		if err != nil {
			return nil, err
		}

		if p.current().Type != TokenRParen {
			return nil, fmt.Errorf("expected ')' at position %d", p.current().Pos)
		}
		p.advance() // consume )

		return inner, nil
	}

	// Check for exists function
	if p.current().Type == TokenExists {
		return p.parseExistsFunc()
	}

	// Parse condition
	return p.parseCondition()
}

// parseExistsFunc parses: "exists" "(" field ")"
func (p *Parser) parseExistsFunc() (*client.Filter, error) {
	p.advance() // consume exists

	if p.current().Type != TokenLParen {
		return nil, fmt.Errorf("expected '(' after 'exists' at position %d", p.current().Pos)
	}
	p.advance() // consume (

	if p.current().Type != TokenField {
		return nil, fmt.Errorf("expected field name in exists() at position %d", p.current().Pos)
	}
	field := p.current().Value
	p.advance() // consume field

	if p.current().Type != TokenRParen {
		return nil, fmt.Errorf("expected ')' after field in exists() at position %d", p.current().Pos)
	}
	p.advance() // consume )

	return &client.Filter{
		Field: field,
		Op:    operator.Exists,
	}, nil
}

// parseCondition parses: field operator value
func (p *Parser) parseCondition() (*client.Filter, error) {
	if p.current().Type != TokenField {
		return nil, fmt.Errorf("expected field at position %d, got %v", p.current().Pos, p.current())
	}
	field := p.current().Value
	p.advance()

	if p.current().Type != TokenOperator {
		return nil, fmt.Errorf("expected operator after field '%s' at position %d", field, p.current().Pos)
	}
	opSymbol := p.current().Value
	p.advance()

	if p.current().Type != TokenValue {
		return nil, fmt.Errorf("expected value after operator at position %d", p.current().Pos)
	}
	value := p.current().Value
	p.advance()

	// Map operator symbol to internal operator
	op, negate := mapOperator(opSymbol)

	return &client.Filter{
		Field:  field,
		Op:     op,
		Value:  value,
		Negate: negate,
	}, nil
}

// mapOperator maps a symbol operator to internal operator and negation flag
func mapOperator(symbol string) (string, bool) {
	switch symbol {
	case "!~=":
		return operator.Regex, true
	case "~=":
		return operator.Regex, false
	case "!=":
		return operator.Equals, true
	case ">=":
		return operator.Gte, false
	case "<=":
		return operator.Lte, false
	case ">":
		return operator.Gt, false
	case "<":
		return operator.Lt, false
	case "=":
		return operator.Equals, false
	default:
		return operator.Equals, false
	}
}

// ParseQueryExpression is the main entry point for parsing a complex query expression
func ParseQueryExpression(expr string) (*client.Filter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}

	lexer := NewLexer(expr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, fmt.Errorf("lexer error: %w", err)
	}

	parser := NewParser(tokens)
	filter, err := parser.ParseQuery()
	if err != nil {
		return nil, fmt.Errorf("parser error: %w", err)
	}

	return filter, nil
}
