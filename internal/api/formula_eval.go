package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"sec/internal/fundamentals"
	"sec/internal/market"
	"sec/internal/screener"
	"sec/internal/store"
)

// FormulaValue represents a typed value produced during formula evaluation.
type FormulaValue struct {
	Kind    string  // "number", "string", "bool", "missing", "unknown"
	Num     float64
	Str     string
	Bool    bool
	Present bool
}

func missingFormulaVal() FormulaValue { return FormulaValue{Kind: "missing"} }
func unknownFormulaVal() FormulaValue { return FormulaValue{Kind: "unknown"} }
func numFormulaVal(v float64) FormulaValue {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return missingFormulaVal()
	}
	return FormulaValue{Kind: "number", Num: v, Present: true}
}
func boolFormulaVal(b bool) FormulaValue {
	return FormulaValue{Kind: "bool", Bool: b, Present: true}
}
func strFormulaVal(s string) FormulaValue {
	return FormulaValue{Kind: "string", Str: s, Present: true}
}

type formulaTokenKind int

const (
	tokEOF formulaTokenKind = iota
	tokNumber
	tokString
	tokIdent
	tokBool
	tokLParen
	tokRParen
	tokComma
	tokPlus
	tokMinus
	tokMul
	tokDiv
	tokNot
	tokAnd
	tokOr
	tokEq
	tokNeq
	tokLt
	tokLte
	tokGt
	tokGte
)

type formulaToken struct {
	kind formulaTokenKind
	text string
	num  float64
	b    bool
}

func tokenizeFormula(src string) ([]formulaToken, error) {
	var tokens []formulaToken
	i := 0
	n := len(src)

	for i < n {
		r := rune(src[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		switch src[i] {
		case '(':
			tokens = append(tokens, formulaToken{kind: tokLParen, text: "("})
			i++
		case ')':
			tokens = append(tokens, formulaToken{kind: tokRParen, text: ")"})
			i++
		case ',':
			tokens = append(tokens, formulaToken{kind: tokComma, text: ","})
			i++
		case '+':
			tokens = append(tokens, formulaToken{kind: tokPlus, text: "+"})
			i++
		case '-':
			tokens = append(tokens, formulaToken{kind: tokMinus, text: "-"})
			i++
		case '*':
			tokens = append(tokens, formulaToken{kind: tokMul, text: "*"})
			i++
		case '/':
			tokens = append(tokens, formulaToken{kind: tokDiv, text: "/"})
			i++
		case '&':
			if i+1 < n && src[i+1] == '&' {
				tokens = append(tokens, formulaToken{kind: tokAnd, text: "&&"})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected character '&' at position %d (use && or AND)", i)
			}
		case '|':
			if i+1 < n && src[i+1] == '|' {
				tokens = append(tokens, formulaToken{kind: tokOr, text: "||"})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected character '|' at position %d (use || or OR)", i)
			}
		case '=':
			if i+1 < n && src[i+1] == '=' {
				tokens = append(tokens, formulaToken{kind: tokEq, text: "=="})
				i += 2
			} else {
				tokens = append(tokens, formulaToken{kind: tokEq, text: "="})
				i++
			}
		case '!':
			if i+1 < n && src[i+1] == '=' {
				tokens = append(tokens, formulaToken{kind: tokNeq, text: "!="})
				i += 2
			} else {
				tokens = append(tokens, formulaToken{kind: tokNot, text: "!"})
				i++
			}
		case '<':
			if i+1 < n && src[i+1] == '=' {
				tokens = append(tokens, formulaToken{kind: tokLte, text: "<="})
				i += 2
			} else if i+1 < n && src[i+1] == '>' {
				tokens = append(tokens, formulaToken{kind: tokNeq, text: "<>"})
				i += 2
			} else {
				tokens = append(tokens, formulaToken{kind: tokLt, text: "<"})
				i++
			}
		case '>':
			if i+1 < n && src[i+1] == '=' {
				tokens = append(tokens, formulaToken{kind: tokGte, text: ">="})
				i += 2
			} else {
				tokens = append(tokens, formulaToken{kind: tokGt, text: ">"})
				i++
			}
		case '"', '\'':
			quote := src[i]
			i++
			var sb strings.Builder
			closed := false
			for i < n {
				ch := src[i]
				if ch == '\\' && i+1 < n {
					i++
					sb.WriteByte(src[i])
					i++
					continue
				}
				if ch == quote {
					i++
					closed = true
					break
				}
				sb.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string literal")
			}
			tokens = append(tokens, formulaToken{kind: tokString, text: sb.String()})
		default:
			if (src[i] >= '0' && src[i] <= '9') || (src[i] == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9') {
				start := i
				for i < n && ((src[i] >= '0' && src[i] <= '9') || src[i] == '.' || src[i] == '_') {
					i++
				}
				if i < n && (src[i] == 'e' || src[i] == 'E') {
					i++
					if i < n && (src[i] == '+' || src[i] == '-') {
						i++
					}
					for i < n && src[i] >= '0' && src[i] <= '9' {
						i++
					}
				}
				numStr := strings.ReplaceAll(src[start:i], "_", "")
				val, err := strconv.ParseFloat(numStr, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid number %q: %w", src[start:i], err)
				}
				if i < n {
					switch src[i] {
					case '%':
						val *= 0.01
						i++
					case 'k', 'K':
						val *= 1e3
						i++
					case 'm', 'M':
						val *= 1e6
						i++
					case 'b', 'B':
						val *= 1e9
						i++
					case 't', 'T':
						val *= 1e12
						i++
					}
				}
				tokens = append(tokens, formulaToken{kind: tokNumber, text: src[start:i], num: val})
			} else if unicode.IsLetter(r) || src[i] == '_' {
				start := i
				for i < n {
					c := src[i]
					if unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_' || c == '&' {
						i++
					} else {
						break
					}
				}
				ident := src[start:i]
				upper := strings.ToUpper(ident)
				switch upper {
				case "AND":
					tokens = append(tokens, formulaToken{kind: tokAnd, text: ident})
				case "OR":
					tokens = append(tokens, formulaToken{kind: tokOr, text: ident})
				case "NOT":
					tokens = append(tokens, formulaToken{kind: tokNot, text: ident})
				case "TRUE":
					tokens = append(tokens, formulaToken{kind: tokBool, text: ident, b: true})
				case "FALSE":
					tokens = append(tokens, formulaToken{kind: tokBool, text: ident, b: false})
				default:
					tokens = append(tokens, formulaToken{kind: tokIdent, text: ident})
				}
			} else {
				return nil, fmt.Errorf("unexpected character %q at position %d", src[i], i)
			}
		}
	}
	tokens = append(tokens, formulaToken{kind: tokEOF})
	return tokens, nil
}

type formulaNode interface {
	collectIdents(seen map[string]struct{}, out *[]string)
}

type numNode struct{ val float64 }
type strNode struct{ val string }
type boolNode struct{ val bool }
type identNode struct{ name string }
type unaryNode struct {
	op    formulaTokenKind
	child formulaNode
}
type binaryNode struct {
	op    formulaTokenKind
	left  formulaNode
	right formulaNode
}
type callNode struct {
	name string
	args []formulaNode
}

func (n numNode) collectIdents(map[string]struct{}, *[]string)  {}
func (n strNode) collectIdents(map[string]struct{}, *[]string)  {}
func (n boolNode) collectIdents(map[string]struct{}, *[]string) {}
func (n identNode) collectIdents(seen map[string]struct{}, out *[]string) {
	key := canonicalFormulaField(n.name)
	if _, ok := seen[key]; !ok {
		seen[key] = struct{}{}
		*out = append(*out, key)
	}
}
func (n unaryNode) collectIdents(seen map[string]struct{}, out *[]string) {
	n.child.collectIdents(seen, out)
}
func (n binaryNode) collectIdents(seen map[string]struct{}, out *[]string) {
	n.left.collectIdents(seen, out)
	n.right.collectIdents(seen, out)
}
func (n callNode) collectIdents(seen map[string]struct{}, out *[]string) {
	for _, a := range n.args {
		a.collectIdents(seen, out)
	}
}

type formulaParser struct {
	tokens []formulaToken
	pos    int
}

func (p *formulaParser) peek() formulaToken {
	if p.pos >= len(p.tokens) {
		return formulaToken{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *formulaParser) next() formulaToken {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

// FormulaExpr is a compiled boolean or numeric formula expression.
type FormulaExpr struct {
	Source  string
	root    formulaNode
	metrics []string
}

// ParseFormula compiles a filter or ranking formula expression.
func ParseFormula(source string) (*FormulaExpr, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return nil, fmt.Errorf("formula expression cannot be empty")
	}
	tokens, err := tokenizeFormula(trimmed)
	if err != nil {
		return nil, err
	}
	p := &formulaParser{tokens: tokens}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().text)
	}

	seen := make(map[string]struct{})
	var metrics []string
	root.collectIdents(seen, &metrics)
	for _, m := range metrics {
		if !isKnownFormulaField(m) {
			return nil, fmt.Errorf("unknown metric %q in formula (run \"sec screen --help\" for available metrics)", m)
		}
	}
	return &FormulaExpr{
		Source:  trimmed,
		root:    root,
		metrics: metrics,
	}, nil
}

// ReferencedMetrics returns the canonical metric names referenced in the formula.
func (f *FormulaExpr) ReferencedMetrics() []string {
	if f == nil {
		return nil
	}
	return append([]string(nil), f.metrics...)
}

// NeedsQuotes reports whether any referenced metric requires live/cached market prices.
func (f *FormulaExpr) NeedsQuotes() bool {
	if f == nil {
		return false
	}
	for _, m := range f.metrics {
		if isQuoteDependentField(m) {
			return true
		}
	}
	return false
}

func (p *formulaParser) parseOr() (formulaNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		op := p.next().kind
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *formulaParser) parseAnd() (formulaNode, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokAnd {
		op := p.next().kind
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *formulaParser) parseComparison() (formulaNode, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	switch p.peek().kind {
	case tokEq, tokNeq, tokLt, tokLte, tokGt, tokGte:
		op := p.next().kind
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		return binaryNode{op: op, left: left, right: right}, nil
	}
	return left, nil
}

func (p *formulaParser) parseAdditive() (formulaNode, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokPlus || p.peek().kind == tokMinus {
		op := p.next().kind
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *formulaParser) parseMultiplicative() (formulaNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokMul || p.peek().kind == tokDiv {
		op := p.next().kind
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *formulaParser) parseUnary() (formulaNode, error) {
	switch p.peek().kind {
	case tokNot, tokPlus, tokMinus:
		op := p.next().kind
		child, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: op, child: child}, nil
	}
	return p.parsePrimary()
}

func (p *formulaParser) parsePrimary() (formulaNode, error) {
	tok := p.peek()
	switch tok.kind {
	case tokLParen:
		p.next()
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("expected closing ')'")
		}
		p.next()
		return node, nil
	case tokNumber:
		p.next()
		return numNode{val: tok.num}, nil
	case tokString:
		p.next()
		return strNode{val: tok.text}, nil
	case tokBool:
		p.next()
		return boolNode{val: tok.b}, nil
	case tokIdent:
		p.next()
		if p.peek().kind == tokLParen {
			p.next()
			var args []formulaNode
			if p.peek().kind != tokRParen {
				for {
					arg, err := p.parseOr()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
					if p.peek().kind == tokComma {
						p.next()
						continue
					}
					break
				}
			}
			if p.peek().kind != tokRParen {
				return nil, fmt.Errorf("expected ')' after function %s arguments", tok.text)
			}
			p.next()
			fnName := strings.ToLower(tok.text)
			switch fnName {
			case "has", "abs", "min", "max", "coalesce":
			default:
				return nil, fmt.Errorf("unknown function %q", tok.text)
			}
			if (fnName == "has" || fnName == "abs") && len(args) != 1 {
				return nil, fmt.Errorf("function %s expects 1 argument, got %d", fnName, len(args))
			}
			if (fnName == "min" || fnName == "max" || fnName == "coalesce") && len(args) < 2 {
				return nil, fmt.Errorf("function %s expects at least 2 arguments", fnName)
			}
			return callNode{name: fnName, args: args}, nil
		}
		return identNode{name: tok.text}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q", tok.text)
	}
}

// canonicalFormulaField maps user-facing query identifiers to canonical keys.
func canonicalFormulaField(raw string) string {
	key := strings.ToUpper(strings.TrimSpace(raw))
	switch key {
	case "ROIC":
		return "ROIC"
	case "ADJ_ROIC", "ADJUSTED_ROIC":
		return "ADJ_ROIC"
	case "AVG_ADJ_ROIC", "ADJ_ROIC_5Y":
		return "AVG_ADJ_ROIC"
	case "GAAP_ROIC", "ROIC_5Y":
		return "GAAP_ROIC"
	case "ROIC_DELTA", "ROIC_SPREAD":
		return "ROIC_DELTA"
	case "SURGING", "SURGING_ROIC":
		return "SURGING"
	case "ROE":
		return "ROE"
	case "SCORE", "QUALITY_SCORE":
		return "SCORE"
	case "PRICE", "SHARE_PRICE":
		return "PRICE"
	case "MARKET_CAP", "MCAP":
		return "MARKET_CAP"
	case "EV", "ENTERPRISE_VALUE":
		return "EV"
	case "PE", "PE_RATIO", "P_E":
		return "PE"
	case "PB", "PRICE_BOOK":
		return "PB"
	case "FCF_YIELD":
		return "FCF_YIELD"
	case "OWNER_YIELD":
		return "OWNER_YIELD"
	case "STANDARD_OWNER_YIELD", "STD_OWNER_YIELD":
		return "STANDARD_OWNER_YIELD"
	case "NET_CASH_YIELD":
		return "NET_CASH_YIELD"
	case "EV_FCF":
		return "EV_FCF"
	case "EV_EBIT":
		return "EV_EBIT"
	case "EV_SALES", "EV_REVENUE":
		return "EV_SALES"
	case "REVENUE", "REV", "SALES", "TOPLINE":
		return "REVENUE"
	case "COST_OF_REVENUE", "COGS", "COR":
		return "COST_OF_REVENUE"
	case "GROSS_PROFIT", "GP":
		return "GROSS_PROFIT"
	case "GROSS_MARGIN", "GM":
		return "GROSS_MARGIN"
	case "OPERATING_INCOME", "OP_INC", "EBIT":
		return "OPERATING_INCOME"
	case "ADJ_EBIT", "ADJUSTED_EBIT", "ADJ_OPERATING_INCOME":
		return "ADJ_EBIT"
	case "OPERATING_MARGIN", "OP_MARGIN", "EBIT_MARGIN":
		return "OPERATING_MARGIN"
	case "ADJ_EBIT_MARGIN":
		return "ADJ_EBIT_MARGIN"
	case "OPERATING_CONSISTENCY", "MARGIN_CONSISTENCY":
		return "OPERATING_CONSISTENCY"
	case "NET_INCOME", "NI", "EARNINGS", "PROFIT":
		return "NET_INCOME"
	case "NET_MARGIN", "PROFIT_MARGIN":
		return "NET_MARGIN"
	case "SGA", "SG&A", "SELLING_GENERAL_ADMINISTRATIVE":
		return "SGA"
	case "RD", "R&D", "RESEARCH_DEVELOPMENT":
		return "RD"
	case "OCF", "OPERATING_CASH_FLOW", "CASH_FLOW":
		return "OCF"
	case "CAPEX", "CAPITAL_EXPENDITURE":
		return "CAPEX"
	case "DNA", "D&A", "DEPRECIATION", "DEPRECIATION_AMORTIZATION":
		return "DNA"
	case "FCF", "FREE_CASH_FLOW":
		return "FCF"
	case "FCF_MARGIN":
		return "FCF_MARGIN"
	case "SBC", "STOCK_BASED_COMPENSATION":
		return "SBC"
	case "SBC_PCT_FCF", "SBC_OF_FCF":
		return "SBC_PCT_FCF"
	case "OWNER_EARNINGS", "SBC_ADJ_FCF":
		return "OWNER_EARNINGS"
	case "ADJ_OWNER_EARNINGS":
		return "ADJ_OWNER_EARNINGS"
	case "OWNER_MARGIN", "OWNER_EARNINGS_MARGIN":
		return "OWNER_MARGIN"
	case "CASH", "CASH_AND_EQUIVALENTS":
		return "CASH"
	case "DEBT", "BORROWINGS", "TOTAL_DEBT":
		return "DEBT"
	case "NET_DEBT":
		return "NET_DEBT"
	case "NET_CASH":
		return "NET_CASH"
	case "NET_CASH_ASSETS", "BALANCE_SHEET_QUALITY":
		return "NET_CASH_ASSETS"
	case "EQUITY", "STOCKHOLDERS_EQUITY", "BOOK_VALUE":
		return "EQUITY"
	case "TOTAL_ASSETS", "ASSETS":
		return "TOTAL_ASSETS"
	case "CURRENT_ASSETS":
		return "CURRENT_ASSETS"
	case "CURRENT_LIABILITIES":
		return "CURRENT_LIABILITIES"
	case "WORKING_CAPITAL", "NWC":
		return "NWC"
	case "INTANGIBLE_CAPITAL", "INTANGIBLES":
		return "INTANGIBLE_CAPITAL"
	case "INTANGIBLE_AMORT", "AMORTIZATION":
		return "INTANGIBLE_AMORT"
	case "ADJ_INVESTED_CAPITAL", "INVESTED_CAPITAL":
		return "ADJ_INVESTED_CAPITAL"
	case "REVENUE_CAGR", "REV_CAGR", "GROWTH":
		return "REVENUE_CAGR"
	case "EPS_GROWTH", "EPS_CAGR":
		return "EPS_GROWTH"
	case "BOOK_GROWTH", "BV_GROWTH":
		return "BOOK_GROWTH"
	case "SHARE_REDUCTION", "BUYBACK_RATE":
		return "SHARE_REDUCTION"
	case "SHARES", "SHARES_OUTSTANDING":
		return "SHARES"
	case "DILUTED_SHARES":
		return "DILUTED_SHARES"
	case "TICKER":
		return "TICKER"
	case "COMPANY", "NAME":
		return "COMPANY"
	case "SECTOR":
		return "SECTOR"
	case "SIC":
		return "SIC"
	case "PEER_GROUP":
		return "PEER_GROUP"
	case "CURRENCY":
		return "CURRENCY"
	default:
		return key
	}
}

var knownFormulaFields = map[string]bool{
	"ROIC": true, "ADJ_ROIC": true, "AVG_ADJ_ROIC": true, "GAAP_ROIC": true,
	"ROIC_DELTA": true, "SURGING": true, "ROE": true, "SCORE": true,
	"PRICE": true, "MARKET_CAP": true, "EV": true, "PE": true, "PB": true,
	"FCF_YIELD": true, "OWNER_YIELD": true, "STANDARD_OWNER_YIELD": true, "NET_CASH_YIELD": true,
	"EV_FCF": true, "EV_EBIT": true, "EV_SALES": true,
	"REVENUE": true, "COST_OF_REVENUE": true, "GROSS_PROFIT": true, "GROSS_MARGIN": true,
	"OPERATING_INCOME": true, "ADJ_EBIT": true, "OPERATING_MARGIN": true, "ADJ_EBIT_MARGIN": true,
	"OPERATING_CONSISTENCY": true, "NET_INCOME": true, "NET_MARGIN": true,
	"SGA": true, "RD": true, "OCF": true, "CAPEX": true, "DNA": true,
	"FCF": true, "FCF_MARGIN": true, "SBC": true, "SBC_PCT_FCF": true,
	"OWNER_EARNINGS": true, "ADJ_OWNER_EARNINGS": true, "OWNER_MARGIN": true,
	"CASH": true, "DEBT": true, "NET_DEBT": true, "NET_CASH": true, "NET_CASH_ASSETS": true,
	"EQUITY": true, "TOTAL_ASSETS": true, "CURRENT_ASSETS": true, "CURRENT_LIABILITIES": true, "NWC": true,
	"INTANGIBLE_CAPITAL": true, "INTANGIBLE_AMORT": true, "ADJ_INVESTED_CAPITAL": true,
	"REVENUE_CAGR": true, "EPS_GROWTH": true, "BOOK_GROWTH": true, "SHARE_REDUCTION": true,
	"SHARES": true, "DILUTED_SHARES": true,
	"TICKER": true, "COMPANY": true, "SECTOR": true, "SIC": true, "PEER_GROUP": true, "CURRENCY": true,
}

func isKnownFormulaField(canonical string) bool {
	return knownFormulaFields[canonical]
}

func isQuoteDependentField(canonical string) bool {
	switch canonical {
	case "PRICE", "MARKET_CAP", "EV", "PE", "PB",
		"FCF_YIELD", "OWNER_YIELD", "STANDARD_OWNER_YIELD", "NET_CASH_YIELD",
		"EV_FCF", "EV_EBIT", "EV_SALES":
		return true
	default:
		return false
	}
}

// FormulaMetricList returns a sorted list of primary formula metric names for docs/completion.
func FormulaMetricList() []string {
	out := make([]string, 0, len(knownFormulaFields))
	for k := range knownFormulaFields {
		out = append(out, strings.ToLower(k))
	}
	sort.Strings(out)
	return out
}

// FormulaCompanyRow holds all precomputed + summary + optional quote fields for fast evaluation.
type FormulaCompanyRow struct {
	CIK        string
	Ticker     string
	Company    string
	SIC        string
	Sector     string
	PeerGroup  string
	Currency   string
	Price      float64
	HasPrice   bool
	nums       map[string]float64
}

func (r *FormulaCompanyRow) Field(canonical string, quotesReady bool) FormulaValue {
	switch canonical {
	case "TICKER":
		return strFormulaVal(r.Ticker)
	case "COMPANY":
		return strFormulaVal(r.Company)
	case "SECTOR":
		return strFormulaVal(r.Sector)
	case "SIC":
		return strFormulaVal(r.SIC)
	case "PEER_GROUP":
		return strFormulaVal(r.PeerGroup)
	case "CURRENCY":
		return strFormulaVal(r.Currency)
	case "SURGING":
		if v, ok := r.nums["SURGING"]; ok {
			return boolFormulaVal(v > 0)
		}
		return missingFormulaVal()
	}

	if isQuoteDependentField(canonical) {
		if !quotesReady {
			return unknownFormulaVal()
		}
		if !r.HasPrice || r.Price <= 0 {
			return missingFormulaVal()
		}
		return r.evalQuoteMetric(canonical)
	}

	if v, ok := r.nums[canonical]; ok {
		return numFormulaVal(v)
	}
	return missingFormulaVal()
}

func (r *FormulaCompanyRow) evalQuoteMetric(canonical string) FormulaValue {
	shares, hasShares := r.nums["SHARES"]
	if !hasShares || shares <= 0 {
		return missingFormulaVal()
	}
	adrShares := market.OrdinaryToADRShares(r.Ticker, shares)
	if adrShares <= 0 {
		return missingFormulaVal()
	}
	mcap := r.Price * adrShares
	if mcap <= 0 || math.IsNaN(mcap) || math.IsInf(mcap, 0) {
		return missingFormulaVal()
	}

	switch canonical {
	case "PRICE":
		return numFormulaVal(r.Price)
	case "MARKET_CAP":
		return numFormulaVal(mcap)
	case "EV":
		debt := r.nums["DEBT"]
		cash := r.nums["CASH"]
		return numFormulaVal(mcap + debt - cash)
	case "PE":
		ni, ok := r.nums["NET_INCOME"]
		if !ok || ni <= 0 {
			return missingFormulaVal()
		}
		return numFormulaVal(mcap / ni)
	case "PB":
		eq, ok := r.nums["EQUITY"]
		if !ok || eq <= 0 {
			return missingFormulaVal()
		}
		return numFormulaVal(mcap / eq)
	case "FCF_YIELD":
		fcf, ok := r.nums["FCF"]
		if !ok {
			return missingFormulaVal()
		}
		return numFormulaVal(fcf / mcap)
	case "OWNER_YIELD":
		aoe, ok := r.nums["ADJ_OWNER_EARNINGS"]
		if !ok {
			return missingFormulaVal()
		}
		return numFormulaVal(aoe / mcap)
	case "STANDARD_OWNER_YIELD":
		oe, ok := r.nums["OWNER_EARNINGS"]
		if !ok {
			return missingFormulaVal()
		}
		return numFormulaVal(oe / mcap)
	case "NET_CASH_YIELD":
		cash, okC := r.nums["CASH"]
		debt := r.nums["DEBT"]
		if !okC {
			return missingFormulaVal()
		}
		return numFormulaVal((cash - debt) / mcap)
	case "EV_FCF":
		fcf, ok := r.nums["FCF"]
		if !ok || fcf <= 0 {
			return missingFormulaVal()
		}
		ev := mcap + r.nums["DEBT"] - r.nums["CASH"]
		return numFormulaVal(ev / fcf)
	case "EV_EBIT":
		ebit, ok := r.nums["OPERATING_INCOME"]
		if !ok || ebit <= 0 {
			return missingFormulaVal()
		}
		ev := mcap + r.nums["DEBT"] - r.nums["CASH"]
		return numFormulaVal(ev / ebit)
	case "EV_SALES":
		rev, ok := r.nums["REVENUE"]
		if !ok || rev <= 0 {
			return missingFormulaVal()
		}
		ev := mcap + r.nums["DEBT"] - r.nums["CASH"]
		return numFormulaVal(ev / rev)
	}
	return missingFormulaVal()
}

// Eval evaluates the compiled formula against a company row.
// When quotesReady is false, quote-dependent metrics yield Kind="unknown", allowing
// short-circuiting of pure-fundamental filters before any quote network requests!
func (f *FormulaExpr) Eval(row *FormulaCompanyRow, quotesReady bool) FormulaValue {
	if f == nil || f.root == nil {
		return boolFormulaVal(true)
	}
	return evalFormulaNode(f.root, row, quotesReady)
}

// Matches evaluates a boolean predicate formula against row.
func (f *FormulaExpr) Matches(row *FormulaCompanyRow, quotesReady bool) (matched bool, unknown bool) {
	res := f.Eval(row, quotesReady)
	switch res.Kind {
	case "unknown":
		return false, true
	case "bool":
		return res.Bool, false
	case "number":
		return res.Num != 0, false
	default:
		return false, false
	}
}

func evalFormulaNode(node formulaNode, row *FormulaCompanyRow, quotesReady bool) FormulaValue {
	switch n := node.(type) {
	case numNode:
		return numFormulaVal(n.val)
	case strNode:
		return strFormulaVal(n.val)
	case boolNode:
		return boolFormulaVal(n.val)
	case identNode:
		return row.Field(canonicalFormulaField(n.name), quotesReady)
	case unaryNode:
		val := evalFormulaNode(n.child, row, quotesReady)
		if val.Kind == "unknown" {
			return unknownFormulaVal()
		}
		switch n.op {
		case tokNot:
			if val.Kind == "missing" {
				return missingFormulaVal()
			}
			if val.Kind == "bool" {
				return boolFormulaVal(!val.Bool)
			}
			if val.Kind == "number" {
				return boolFormulaVal(val.Num == 0)
			}
			return missingFormulaVal()
		case tokPlus:
			if val.Kind == "number" {
				return val
			}
			return missingFormulaVal()
		case tokMinus:
			if val.Kind == "number" {
				return numFormulaVal(-val.Num)
			}
			return missingFormulaVal()
		}
	case callNode:
		switch n.name {
		case "has":
			if ident, ok := n.args[0].(identNode); ok {
				v := row.Field(canonicalFormulaField(ident.name), quotesReady)
				if v.Kind == "unknown" {
					return unknownFormulaVal()
				}
				return boolFormulaVal(v.Present)
			}
			v := evalFormulaNode(n.args[0], row, quotesReady)
			if v.Kind == "unknown" {
				return unknownFormulaVal()
			}
			return boolFormulaVal(v.Present)
		case "abs":
			v := evalFormulaNode(n.args[0], row, quotesReady)
			if v.Kind == "unknown" {
				return unknownFormulaVal()
			}
			if v.Kind == "number" {
				return numFormulaVal(math.Abs(v.Num))
			}
			return missingFormulaVal()
		case "min", "max":
			var best float64
			hasBest := false
			for _, arg := range n.args {
				v := evalFormulaNode(arg, row, quotesReady)
				if v.Kind == "unknown" {
					return unknownFormulaVal()
				}
				if v.Kind != "number" {
					return missingFormulaVal()
				}
				if !hasBest || (n.name == "min" && v.Num < best) || (n.name == "max" && v.Num > best) {
					best = v.Num
					hasBest = true
				}
			}
			if !hasBest {
				return missingFormulaVal()
			}
			return numFormulaVal(best)
		case "coalesce":
			for _, arg := range n.args {
				v := evalFormulaNode(arg, row, quotesReady)
				if v.Kind == "unknown" {
					return unknownFormulaVal()
				}
				if v.Present {
					return v
				}
			}
			return missingFormulaVal()
		}
	case binaryNode:
		switch n.op {
		case tokAnd:
			l := evalFormulaNode(n.left, row, quotesReady)
			if l.Kind == "bool" && !l.Bool {
				return boolFormulaVal(false)
			}
			if l.Kind == "missing" {
				return boolFormulaVal(false)
			}
			r := evalFormulaNode(n.right, row, quotesReady)
			if r.Kind == "bool" && !r.Bool {
				return boolFormulaVal(false)
			}
			if r.Kind == "missing" {
				return boolFormulaVal(false)
			}
			if l.Kind == "unknown" || r.Kind == "unknown" {
				return unknownFormulaVal()
			}
			return boolFormulaVal(truthy(l) && truthy(r))
		case tokOr:
			l := evalFormulaNode(n.left, row, quotesReady)
			if l.Kind == "bool" && l.Bool {
				return boolFormulaVal(true)
			}
			r := evalFormulaNode(n.right, row, quotesReady)
			if r.Kind == "bool" && r.Bool {
				return boolFormulaVal(true)
			}
			if l.Kind == "unknown" || r.Kind == "unknown" {
				return unknownFormulaVal()
			}
			return boolFormulaVal(truthy(l) || truthy(r))
		}

		l := evalFormulaNode(n.left, row, quotesReady)
		r := evalFormulaNode(n.right, row, quotesReady)
		if l.Kind == "unknown" || r.Kind == "unknown" {
			return unknownFormulaVal()
		}
		if l.Kind == "missing" || r.Kind == "missing" {
			return missingFormulaVal()
		}

		switch n.op {
		case tokPlus:
			if l.Kind == "number" && r.Kind == "number" {
				return numFormulaVal(l.Num + r.Num)
			}
		case tokMinus:
			if l.Kind == "number" && r.Kind == "number" {
				return numFormulaVal(l.Num - r.Num)
			}
		case tokMul:
			if l.Kind == "number" && r.Kind == "number" {
				return numFormulaVal(l.Num * r.Num)
			}
		case tokDiv:
			if l.Kind == "number" && r.Kind == "number" && r.Num != 0 {
				return numFormulaVal(l.Num / r.Num)
			}
			return missingFormulaVal()
		case tokEq, tokNeq, tokLt, tokLte, tokGt, tokGte:
			return evalCompare(n.op, l, r)
		}
	}
	return missingFormulaVal()
}

func truthy(v FormulaValue) bool {
	switch v.Kind {
	case "bool":
		return v.Bool
	case "number":
		return v.Num != 0
	case "string":
		return v.Str != ""
	default:
		return false
	}
}

func evalCompare(op formulaTokenKind, l, r FormulaValue) FormulaValue {
	if l.Kind == "string" && r.Kind == "string" {
		cmp := strings.Compare(strings.ToUpper(l.Str), strings.ToUpper(r.Str))
		switch op {
		case tokEq:
			return boolFormulaVal(cmp == 0)
		case tokNeq:
			return boolFormulaVal(cmp != 0)
		case tokLt:
			return boolFormulaVal(cmp < 0)
		case tokLte:
			return boolFormulaVal(cmp <= 0)
		case tokGt:
			return boolFormulaVal(cmp > 0)
		case tokGte:
			return boolFormulaVal(cmp >= 0)
		}
		return missingFormulaVal()
	}
	if l.Kind == "bool" && r.Kind == "bool" {
		switch op {
		case tokEq:
			return boolFormulaVal(l.Bool == r.Bool)
		case tokNeq:
			return boolFormulaVal(l.Bool != r.Bool)
		}
		return missingFormulaVal()
	}
	if l.Kind == "number" && r.Kind == "number" {
		const eps = 1e-12
		switch op {
		case tokEq:
			return boolFormulaVal(math.Abs(l.Num-r.Num) <= eps)
		case tokNeq:
			return boolFormulaVal(math.Abs(l.Num-r.Num) > eps)
		case tokLt:
			return boolFormulaVal(l.Num < r.Num)
		case tokLte:
			return boolFormulaVal(l.Num <= r.Num)
		case tokGt:
			return boolFormulaVal(l.Num > r.Num)
		case tokGte:
			return boolFormulaVal(l.Num >= r.Num)
		}
	}
	return missingFormulaVal()
}

// FormulaScreenOptions controls formula screening execution.
type FormulaScreenOptions struct {
	Tickers        []string
	Where          string
	SortExpr       string
	SortAsc        bool
	Top            int
	Columns        []string
	MarketCaps     map[string]float64
	Prices         map[string]float64
	Offline        bool
	RefreshQuotes  bool
	OnQuoteHydrate func(ctx context.Context, missingTickers []string) map[string]float64
}

// FormulaScreenCandidate represents one matching company and its evaluated display columns.
type FormulaScreenCandidate struct {
	Ticker   string             `json:"ticker"`
	Company  string             `json:"company"`
	Sector   string             `json:"sector"`
	Currency string             `json:"currency"`
	Metrics  map[string]any     `json:"metrics"`
	sortKey  float64
	hasSort  bool
}

// FormulaScreenResult holds all matched candidates and metadata for rendering.
type FormulaScreenResult struct {
	Where      string                   `json:"where"`
	Columns    []string                 `json:"columns"`
	Considered int                      `json:"considered"`
	Matched    int                      `json:"matched"`
	UsedQuotes bool                     `json:"used_quotes"`
	Candidates []FormulaScreenCandidate `json:"candidates"`
}

// LoadFormulaCompanyRows reads precomputed summaries, rank variables, and GAAP arbitrage variables
// for the requested tickers (or all clean companies in the database) in bulk SQLite queries.
func LoadFormulaCompanyRows(ctx context.Context, s *store.Store, tickers []string) ([]*FormulaCompanyRow, error) {
	cleanTickers := screener.NormalizeTickers(tickers)
	if len(tickers) > 0 && len(cleanTickers) == 0 {
		return nil, nil
	}

	if _, err := screener.PrecomputeMissingOrDirty(ctx, s, screener.PrecomputeOptions{
		Tickers:           cleanTickers,
		RankYears:         []int{6, 5},
		ArbYears:          []int{5},
		RefreshRankScores: len(cleanTickers) == 0,
	}); err != nil {
		return nil, err
	}

	if fxRates, fxErr := s.LoadAllFXRates(ctx); fxErr == nil {
		for _, fx := range fxRates {
			if fx.RateToUSD > 0 {
				market.SetFXRate(fx.Currency, fx.RateToUSD)
			}
		}
	}

	rank6, err := s.LoadRankVariables(ctx, 6, screener.RankSchemaVersion, cleanTickers)
	if err != nil {
		return nil, err
	}
	rank5, err := s.LoadRankVariables(ctx, 5, screener.RankSchemaVersion, cleanTickers)
	if err != nil {
		return nil, err
	}
	arbRows, err := s.LoadGAAPArbitrageVariables(ctx, 5, screener.GAAPArbitrageSchemaVersion, cleanTickers)
	if err != nil {
		return nil, err
	}

	rankByCIK := make(map[string]store.RankVariableRecord, len(rank6)+len(rank5))
	for _, r := range rank5 {
		rankByCIK[r.CIK] = r
	}
	for _, r := range rank6 {
		rankByCIK[r.CIK] = r
	}
	arbByCIK := make(map[string]store.GAAPArbitrageVariableRecord, len(arbRows))
	for _, a := range arbRows {
		arbByCIK[a.CIK] = a
	}

	var tickerSet map[string]struct{}
	if len(cleanTickers) > 0 {
		tickerSet = make(map[string]struct{}, len(cleanTickers))
		for _, t := range cleanTickers {
			tickerSet[t] = struct{}{}
		}
	}

	var qb strings.Builder
	var args []any
	qb.WriteString(`
SELECT cs.cik, t.ticker, cs.company_name, cs.sic, cs.currency,
       cs.latest_share_count, cs.has_latest_share_count, cs.summary_json
FROM sec_company_summaries cs
JOIN sec_tickers t ON t.cik = cs.cik
WHERE cs.dirty = 0 AND cs.schema_version = ? AND cs.periods_count > 0`)
	args = append(args, fundamentals.SummarySchemaVersion)
	useSQLFilter := len(tickerSet) > 0 && len(tickerSet) <= 400
	if useSQLFilter {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(tickerSet)), ",")
		qb.WriteString(" AND t.ticker IN (" + placeholders + ")")
		for t := range tickerSet {
			args = append(args, t)
		}
	}
	qb.WriteString(" ORDER BY t.ticker")

	rows, err := s.DB().QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("load precomputed summaries for formula screen: %w", err)
	}
	defer rows.Close()

	var result []*FormulaCompanyRow
	seenTickers := make(map[string]struct{})
	for rows.Next() {
		var cik, ticker, companyName, sic, currency, summaryJSON string
		var latestShares float64
		var hasLatestShares int
		if err := rows.Scan(&cik, &ticker, &companyName, &sic, &currency, &latestShares, &hasLatestShares, &summaryJSON); err != nil {
			return nil, fmt.Errorf("scan company summary: %w", err)
		}
		upperTicker := strings.ToUpper(strings.TrimSpace(ticker))
		if len(tickerSet) > 0 && !useSQLFilter {
			if _, ok := tickerSet[upperTicker]; !ok {
				continue
			}
		}
		if _, dup := seenTickers[upperTicker]; dup {
			continue
		}
		seenTickers[upperTicker] = struct{}{}

		var summary fundamentals.FinancialSummary
		if err := json.Unmarshal([]byte(summaryJSON), &summary); err != nil {
			continue
		}
		if currency == "" {
			currency = fundamentals.SummaryCurrency(summary)
		}
		if currency == "" {
			currency = "USD"
		}

		row := buildFormulaCompanyRow(cik, upperTicker, companyName, sic, currency, latestShares, hasLatestShares != 0, summary, rankByCIK[cik], arbByCIK[cik])
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func buildFormulaCompanyRow(
	cik, ticker, companyName, sic, currency string,
	latestShares float64,
	hasLatestShares bool,
	summary fundamentals.FinancialSummary,
	rankRec store.RankVariableRecord,
	arbRec store.GAAPArbitrageVariableRecord,
) *FormulaCompanyRow {
	sector := rankRec.Sector
	if sector == "" {
		sector = arbRec.Sector
	}
	if sector == "" {
		if sicNum, err := strconv.Atoi(strings.TrimSpace(sic)); err == nil && sicNum >= 6000 && sicNum <= 6799 {
			sector = "Financials"
		} else if strings.TrimSpace(sic) != "" {
			sector = "Nonfinancial"
		} else {
			sector = "Unclassified"
		}
	}
	peerGroup := rankRec.PeerGroup
	if peerGroup == "" {
		peerGroup = rankRec.RawPeerGroup
	}

	nums := make(map[string]float64, 48)
	toUSD := func(v float64) float64 {
		return market.ToUSD(v, currency)
	}

	setSummaryMoney := func(canonical, summaryMetricName string, absVal bool) {
		vals := fundamentals.NamedMetric(summary, summaryMetricName)
		if len(vals) > 0 && vals[0].Present && !math.IsNaN(vals[0].Value) && !math.IsInf(vals[0].Value, 0) {
			v := vals[0].Value
			if absVal {
				v = math.Abs(v)
			}
			unit := vals[0].Unit
			if unit == "" {
				unit = currency
			}
			nums[canonical] = market.ToUSD(v, unit)
		}
	}

	setSummaryMoney("REVENUE", "Revenue", false)
	setSummaryMoney("COST_OF_REVENUE", "Cost of Revenue", true)
	setSummaryMoney("OPERATING_INCOME", "Operating Income", false)
	setSummaryMoney("ADJ_EBIT", "Adjusted Operating Income", false)
	setSummaryMoney("SGA", "Selling, General & Administrative", true)
	setSummaryMoney("RD", "Research & Development", true)
	setSummaryMoney("NET_INCOME", "Net Income", false)
	setSummaryMoney("OCF", "Operating Cash Flow", false)
	setSummaryMoney("CAPEX", "Capital Expenditure", true)
	setSummaryMoney("DNA", "Depreciation & Amortization", true)
	setSummaryMoney("FCF", "Free Cash Flow", false)
	setSummaryMoney("SBC", "Stock-Based Compensation", true)
	setSummaryMoney("OWNER_EARNINGS", "SBC-Adjusted FCF Proxy", false)
	setSummaryMoney("CASH", "Cash & Equivalents", false)
	setSummaryMoney("DEBT", "Debt / Borrowings", false)
	setSummaryMoney("EQUITY", "Stockholders' Equity", false)
	setSummaryMoney("TOTAL_ASSETS", "Total Assets", false)
	setSummaryMoney("CURRENT_ASSETS", "Current Assets", false)
	setSummaryMoney("CURRENT_LIABILITIES", "Current Liabilities", false)
	setSummaryMoney("INTANGIBLE_CAPITAL", "Intangible Capital", false)
	setSummaryMoney("INTANGIBLE_AMORT", "Amortization of Intangibles", false)
	setSummaryMoney("ADJ_INVESTED_CAPITAL", "Adjusted Invested Capital", false)

	if rev, okRev := nums["REVENUE"]; okRev && rev > 0 {
		if cost, okCost := nums["COST_OF_REVENUE"]; okCost {
			gp := rev - cost
			nums["GROSS_PROFIT"] = gp
			nums["GROSS_MARGIN"] = gp / rev
		}
		if opInc, okOp := nums["OPERATING_INCOME"]; okOp {
			nums["OPERATING_MARGIN"] = opInc / rev
		}
		if adjEBIT, okAdj := nums["ADJ_EBIT"]; okAdj {
			nums["ADJ_EBIT_MARGIN"] = adjEBIT / rev
		}
		if ni, okNI := nums["NET_INCOME"]; okNI {
			nums["NET_MARGIN"] = ni / rev
		}
		if fcf, okFCF := nums["FCF"]; okFCF {
			nums["FCF_MARGIN"] = fcf / rev
		}
		if oe, okOE := nums["OWNER_EARNINGS"]; okOE {
			nums["OWNER_MARGIN"] = oe / rev
		}
	}
	if fcf, okFCF := nums["FCF"]; okFCF && fcf > 0 {
		if sbc, okSBC := nums["SBC"]; okSBC {
			nums["SBC_PCT_FCF"] = sbc / fcf
		}
	}
	if _, hasOE := nums["OWNER_EARNINGS"]; !hasOE {
		if fcf, okFCF := nums["FCF"]; okFCF {
			nums["OWNER_EARNINGS"] = fcf
		}
	}
	if ca, okCA := nums["CURRENT_ASSETS"]; okCA {
		if cl, okCL := nums["CURRENT_LIABILITIES"]; okCL {
			nums["NWC"] = ca - cl
		}
	}
	if cash, okCash := nums["CASH"]; okCash {
		debt := nums["DEBT"]
		nums["NET_DEBT"] = debt - cash
		nums["NET_CASH"] = cash - debt
		if assets, okA := nums["TOTAL_ASSETS"]; okA && assets > 0 {
			nums["NET_CASH_ASSETS"] = (cash - debt) / assets
		}
	}

	// Share counts
	var sharesOut float64
	for _, metricName := range []string{"Shares Outstanding", "Diluted Weighted Average Shares"} {
		vals := fundamentals.NamedMetric(summary, metricName)
		for _, v := range vals {
			if fundamentals.IsShareCount(v) && v.Value > 0 {
				sharesOut = v.Value
				break
			}
		}
		if sharesOut > 0 {
			break
		}
	}
	if sharesOut <= 0 && hasLatestShares && latestShares > 0 {
		sharesOut = latestShares
	}
	if sharesOut <= 0 && arbRec.SharesOutstanding > 0 {
		sharesOut = arbRec.SharesOutstanding
	}
	if sharesOut > 0 {
		nums["SHARES"] = sharesOut
	}
	if dVals := fundamentals.NamedMetric(summary, "Diluted Weighted Average Shares"); len(dVals) > 0 && fundamentals.IsShareCount(dVals[0]) && dVals[0].Value > 0 {
		nums["DILUTED_SHARES"] = dVals[0].Value
	}

	// Adjusted ROIC from summary (1Y) or arbRec
	adjROICVals := fundamentals.NamedMetric(summary, "Intangible-Adjusted ROIC")
	if len(adjROICVals) > 0 && adjROICVals[0].Present {
		nums["ADJ_ROIC"] = adjROICVals[0].Value
	} else if arbRec.HasAdjustedROIC {
		nums["ADJ_ROIC"] = arbRec.AdjustedROIC
	}

	// Precomputed Rank variables (5Y trends, peer score)
	if rankRec.CIK != "" && rankRec.Eligible {
		if rankRec.Score > 0 {
			nums["SCORE"] = rankRec.Score
		}
		if rankRec.HasROIC {
			nums["GAAP_ROIC"] = rankRec.ROIC
		}
		if rankRec.HasROE {
			nums["ROE"] = rankRec.ROE
		}
		if rankRec.HasRevenueGrowth {
			nums["REVENUE_CAGR"] = rankRec.RevenueCAGR
		}
		if rankRec.HasEPSGrowth {
			nums["EPS_GROWTH"] = rankRec.EPSGrowth
		}
		if rankRec.HasBookValueGrowth {
			nums["BOOK_GROWTH"] = rankRec.BookValueGrowth
		}
		if rankRec.HasShareTrend {
			nums["SHARE_REDUCTION"] = rankRec.ShareReduction
		}
		if rankRec.HasOperatingMargin {
			nums["OPERATING_CONSISTENCY"] = rankRec.OperatingConsistency
		}
		if rankRec.HasBalanceSheet {
			nums["NET_CASH_ASSETS"] = rankRec.BalanceSheetQuality
		}
	}

	// Precomputed GAAP Arbitrage variables
	if arbRec.CIK != "" && arbRec.Eligible {
		if _, ok := nums["GAAP_ROIC"]; !ok && arbRec.HasGAAPROIC {
			nums["GAAP_ROIC"] = arbRec.GAAPROIC
		}
		if arbRec.HasAdjustedROIC {
			nums["AVG_ADJ_ROIC"] = arbRec.AvgAdjustedROIC
			if _, ok := nums["ADJ_ROIC"]; !ok {
				nums["ADJ_ROIC"] = arbRec.AdjustedROIC
			}
		}
		if arbRec.HasAdjustedROIC && arbRec.HasGAAPROIC {
			nums["ROIC_DELTA"] = arbRec.ROICDelta
		}
		if arbRec.SurgingROIC {
			nums["SURGING"] = 1
		} else {
			nums["SURGING"] = 0
		}
		if arbRec.HasOwnerEarnings {
			nums["ADJ_OWNER_EARNINGS"] = toUSD(arbRec.AdjustedOwnerEarnings)
		}
	}
	if _, ok := nums["ADJ_OWNER_EARNINGS"]; !ok {
		if oe, okOE := nums["OWNER_EARNINGS"]; okOE {
			rd := nums["RD"]
			sga := nums["SGA"]
			amort := nums["INTANGIBLE_AMORT"]
			nums["ADJ_OWNER_EARNINGS"] = oe + math.Max(0, (rd+0.30*sga)-amort)
		}
	}

	// Canonical ROIC: prefer Intangible-Adjusted ROIC (consistent with api/formula.go), fallback to GAAP ROIC.
	if adj, ok := nums["ADJ_ROIC"]; ok {
		nums["ROIC"] = adj
	} else if gaap, ok := nums["GAAP_ROIC"]; ok {
		nums["ROIC"] = gaap
	}

	return &FormulaCompanyRow{
		CIK:       cik,
		Ticker:    ticker,
		Company:   companyName,
		SIC:       sic,
		Sector:    sector,
		PeerGroup: peerGroup,
		Currency:  currency,
		nums:      nums,
	}
}

// ScreenByFormula screens local companies using a compiled --where expression,
// lazily fetching quotes only for companies that are not already rejected by fundamental filters.
func ScreenByFormula(ctx context.Context, s *store.Store, opts FormulaScreenOptions) (FormulaScreenResult, error) {
	var result FormulaScreenResult
	expr, err := ParseFormula(opts.Where)
	if err != nil {
		return result, err
	}
	result.Where = expr.Source

	var sortExpr *FormulaExpr
	if strings.TrimSpace(opts.SortExpr) != "" {
		sortExpr, err = ParseFormula(opts.SortExpr)
		if err != nil {
			return result, fmt.Errorf("invalid --sort expression: %w", err)
		}
	}

	rows, err := LoadFormulaCompanyRows(ctx, s, opts.Tickers)
	if err != nil {
		return result, err
	}
	result.Considered = len(rows)

	needQuotes := expr.NeedsQuotes() || (sortExpr != nil && sortExpr.NeedsQuotes())
	for _, col := range opts.Columns {
		if isQuoteDependentField(canonicalFormulaField(col)) {
			needQuotes = true
		}
	}

	// Pass 1: Prune companies whose non-quote predicates already evaluate to false.
	var candidatesForPass2 []*FormulaCompanyRow
	var missingQuotes []string
	missingSet := make(map[string]struct{})

	// Pre-populate any explicitly supplied prices/marketCaps or cached quotes.
	cachedQuotes := make(map[string]store.QuoteRecord)
	if needQuotes {
		if qMap, qErr := s.LoadQuotes(ctx, nil); qErr == nil {
			cachedQuotes = qMap
		}
	}

	applyProvidedPrice := func(row *FormulaCompanyRow) {
		if opts.Prices != nil {
			if p, ok := opts.Prices[row.Ticker]; ok && p > 0 {
				row.Price = p
				row.HasPrice = true
				return
			}
		}
		if opts.MarketCaps != nil {
			if mcap, ok := opts.MarketCaps[row.Ticker]; ok && mcap > 0 {
				if shares, okS := row.nums["SHARES"]; okS && shares > 0 {
					adrShares := market.OrdinaryToADRShares(row.Ticker, shares)
					if adrShares > 0 {
						row.Price = market.ToUSD(mcap, row.Currency) / adrShares
						row.HasPrice = true
						return
					}
				}
			}
		}
		if !opts.RefreshQuotes {
			if cq, ok := cachedQuotes[row.Ticker]; ok && cq.Price > 0 {
				row.Price = cq.Price
				row.HasPrice = true
			}
		}
	}

	for _, row := range rows {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !needQuotes {
			matched, _ := expr.Matches(row, true)
			if matched {
				candidatesForPass2 = append(candidatesForPass2, row)
			}
			continue
		}

		applyProvidedPrice(row)
		matched, unknown := expr.Matches(row, row.HasPrice)
		if !matched && !unknown {
			continue
		}
		candidatesForPass2 = append(candidatesForPass2, row)
		if !row.HasPrice {
			if _, seen := missingSet[row.Ticker]; !seen {
				missingSet[row.Ticker] = struct{}{}
				missingQuotes = append(missingQuotes, row.Ticker)
			}
		}
	}

	if needQuotes && len(missingQuotes) > 0 && !opts.Offline && opts.OnQuoteHydrate != nil {
		result.UsedQuotes = true
		livePrices := opts.OnQuoteHydrate(ctx, missingQuotes)
		for _, row := range candidatesForPass2 {
			if !row.HasPrice {
				if p, ok := livePrices[row.Ticker]; ok && p > 0 {
					row.Price = p
					row.HasPrice = true
				}
			}
		}
	} else if needQuotes {
		result.UsedQuotes = true
	}

	// Determine display columns: user columns or metrics referenced in --where + defaults.
	cols := selectDisplayColumns(opts.Columns, expr, sortExpr)
	result.Columns = cols

	// Pass 2: Final evaluation and projection.
	var matched []FormulaScreenCandidate
	for _, row := range candidatesForPass2 {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if ok, _ := expr.Matches(row, true); !ok {
			continue
		}
		metricsMap := make(map[string]any, len(cols))
		for _, col := range cols {
			val := row.Field(col, true)
			if val.Present {
				switch val.Kind {
				case "number":
					metricsMap[col] = val.Num
				case "bool":
					metricsMap[col] = val.Bool
				case "string":
					metricsMap[col] = val.Str
				}
			}
		}
		cand := FormulaScreenCandidate{
			Ticker:   row.Ticker,
			Company:  row.Company,
			Sector:   row.Sector,
			Currency: row.Currency,
			Metrics:  metricsMap,
		}
		if sortExpr != nil {
			sv := sortExpr.Eval(row, true)
			if sv.Kind == "number" {
				cand.sortKey = sv.Num
				cand.hasSort = true
			}
		} else {
			for _, col := range cols {
				if v, ok := metricsMap[col].(float64); ok {
					cand.sortKey = v
					cand.hasSort = true
					break
				}
			}
		}
		matched = append(matched, cand)
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].hasSort != matched[j].hasSort {
			return matched[i].hasSort
		}
		if matched[i].hasSort && matched[i].sortKey != matched[j].sortKey {
			if opts.SortAsc {
				return matched[i].sortKey < matched[j].sortKey
			}
			return matched[i].sortKey > matched[j].sortKey
		}
		return matched[i].Ticker < matched[j].Ticker
	})

	if opts.Top > 0 && opts.Top < len(matched) {
		matched = matched[:opts.Top]
	}
	result.Matched = len(matched)
	result.Candidates = matched
	return result, nil
}

func selectDisplayColumns(userCols []string, whereExpr, sortExpr *FormulaExpr) []string {
	seen := make(map[string]struct{})
	var cols []string
	add := func(col string) {
		c := canonicalFormulaField(col)
		if !isKnownFormulaField(c) {
			return
		}
		if c == "TICKER" || c == "COMPANY" {
			return
		}
		if _, ok := seen[c]; !ok {
			seen[c] = struct{}{}
			cols = append(cols, c)
		}
	}

	if len(userCols) > 0 {
		for _, c := range userCols {
			add(c)
		}
		return cols
	}

	if sortExpr != nil {
		for _, m := range sortExpr.ReferencedMetrics() {
			add(m)
		}
	}
	for _, m := range whereExpr.ReferencedMetrics() {
		add(m)
	}
	defaults := []string{"REVENUE", "ROIC", "OPERATING_MARGIN", "FCF_MARGIN"}
	if whereExpr.NeedsQuotes() || (sortExpr != nil && sortExpr.NeedsQuotes()) {
		defaults = []string{"REVENUE", "ROIC", "FCF_YIELD", "MARKET_CAP"}
	}
	for _, def := range defaults {
		if len(cols) >= 5 {
			break
		}
		add(def)
	}
	return cols
}

// WriteFormulaScreenTable prints FormulaScreenResult as an aligned ASCII table.
func WriteFormulaScreenTable(output io.Writer, res FormulaScreenResult) error {
	if _, err := fmt.Fprintf(output, "Formula Screen (%s) — %d matches of %d evaluated companies\n",
		res.Where, res.Matched, res.Considered); err != nil {
		return err
	}
	if len(res.Candidates) == 0 {
		_, err := fmt.Fprintln(output, "No companies matched the formula criteria.")
		return err
	}

	tw := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprint(tw, "RANK\tTICKER\tCOMPANY\tCCY"); err != nil {
		return err
	}
	for _, col := range res.Columns {
		if _, err := fmt.Fprintf(tw, "\t%s", col); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(tw); err != nil {
		return err
	}

	for idx, c := range res.Candidates {
		name := c.Company
		if len(name) > 26 {
			name = strings.TrimSpace(name[:25]) + "…"
		}
		if _, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s", idx+1, c.Ticker, name, c.Currency); err != nil {
			return err
		}
		for _, col := range res.Columns {
			cell := formatFormulaCell(col, c.Metrics[col])
			if _, err := fmt.Fprintf(tw, "\t%s", cell); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(tw); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatFormulaCell(col string, raw any) string {
	if raw == nil {
		return "—"
	}
	switch v := raw.(type) {
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case string:
		if v == "" {
			return "—"
		}
		return v
	case float64:
		switch col {
		case "ROIC", "ADJ_ROIC", "AVG_ADJ_ROIC", "GAAP_ROIC", "ROIC_DELTA", "ROE",
			"GROSS_MARGIN", "OPERATING_MARGIN", "ADJ_EBIT_MARGIN", "OPERATING_CONSISTENCY",
			"NET_MARGIN", "FCF_MARGIN", "OWNER_MARGIN", "REVENUE_CAGR", "EPS_GROWTH",
			"BOOK_GROWTH", "SHARE_REDUCTION", "NET_CASH_ASSETS",
			"FCF_YIELD", "OWNER_YIELD", "STANDARD_OWNER_YIELD", "NET_CASH_YIELD", "SBC_PCT_FCF":
			return fmt.Sprintf("%.1f%%", v*100)
		case "SCORE":
			return fmt.Sprintf("%.1f", v)
		case "PE", "PB", "EV_FCF", "EV_EBIT", "EV_SALES":
			return fmt.Sprintf("%.1fx", v)
		case "PRICE":
			return fmt.Sprintf("$%.2f", v)
		case "SHARES", "DILUTED_SHARES":
			return formatMetricMagnitude(v)
		default:
			return "$" + formatMetricMagnitude(v)
		}
	default:
		return fmt.Sprint(v)
	}
}
