export type FormulaNode =
  | { kind: 'number'; value: number }
  | { kind: 'metric'; name: string }
  | { kind: 'unary'; operator: '+' | '-'; child: FormulaNode }
  | { kind: 'binary'; operator: '+' | '-' | '*' | '/'; left: FormulaNode; right: FormulaNode }

const metricNames = [
  'Stockholders\' Equity',
  'Diluted Weighted Average Shares',
  'Selling, General & Administrative',
  'Research & Development',
  'SBC-Adjusted FCF Proxy',
  'Accounts Receivable',
  'Current Liabilities',
  'Operating Cash Flow',
  'Capital Expenditure',
  'Stock-Based Compensation',
  'Cash & Equivalents',
  'Debt / Borrowings',
  'Current Assets',
  'Total Assets',
  'Income Before Tax',
  'Income Tax Expense',
  'Enterprise Value',
  'Free Cash Flow',
  'Cost of Revenue',
  'Operating Income',
  'Shares Outstanding',
  'Gross Profit',
  'Interest Expense',
  'Net Income',
  'Revenue',
  'EV',
  'FCF',
  'SBC',
  'R&D',
].sort((a, b) => b.length - a.length)

const metricAliases: Record<string, string> = {
  EV: 'Enterprise Value',
  FCF: 'Free Cash Flow',
  SBC: 'Stock-Based Compensation',
  'R&D': 'Research & Development',
}

class FormulaParser {
  private position = 0

  constructor(private readonly source: string) {}

  parse(): FormulaNode {
    const result = this.parseExpression()
    this.skipWhitespace()
    if (this.position !== this.source.length) {
      throw new Error(`Unexpected token near “${this.source.slice(this.position)}”.`)
    }
    return result
  }

  private parseExpression(): FormulaNode {
    let node = this.parseTerm()
    while (true) {
      this.skipWhitespace()
      const operator = this.source[this.position]
      if (operator !== '+' && operator !== '-') return node
      this.position++
      node = { kind: 'binary', operator, left: node, right: this.parseTerm() }
    }
  }

  private parseTerm(): FormulaNode {
    let node = this.parseUnary()
    while (true) {
      this.skipWhitespace()
      const operator = this.source[this.position]
      if (operator !== '*' && operator !== '/') return node
      this.position++
      node = { kind: 'binary', operator, left: node, right: this.parseUnary() }
    }
  }

  private parseUnary(): FormulaNode {
    this.skipWhitespace()
    const operator = this.source[this.position]
    if (operator === '+' || operator === '-') {
      this.position++
      return { kind: 'unary', operator, child: this.parseUnary() }
    }
    return this.parsePrimary()
  }

  private parsePrimary(): FormulaNode {
    this.skipWhitespace()
    if (this.source[this.position] === '(') {
      this.position++
      const node = this.parseExpression()
      this.skipWhitespace()
      if (this.source[this.position] !== ')') throw new Error('Missing closing parenthesis.')
      this.position++
      return node
    }

    const remaining = this.source.slice(this.position)
    const numberMatch = remaining.match(/^(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?/i)
    if (numberMatch) {
      this.position += numberMatch[0].length
      return { kind: 'number', value: Number(numberMatch[0]) }
    }

    const sourceLower = this.source.toLowerCase()
    const candidate = metricNames.find((name) => {
      const start = this.position
      const end = start + name.length
      if (sourceLower.slice(start, end) !== name.toLowerCase()) return false
      const next = this.source[end]
      return !next || !/[a-z0-9&']/i.test(next)
    })
    if (candidate) {
      this.position += candidate.length
      return { kind: 'metric', name: metricAliases[candidate] || candidate }
    }

    throw new Error(`Expected a metric, number, or “(” near “${remaining.trim()}”.`)
  }

  private skipWhitespace(): void {
    while (/\s/.test(this.source[this.position] || '')) this.position++
  }
}

export const parseCustomFormula = (source: string): FormulaNode => {
  if (!source.trim()) throw new Error('Enter a formula to plot.')
  return new FormulaParser(source).parse()
}

export const hasDivision = (node: FormulaNode): boolean => {
  if (node.kind === 'binary') {
    return node.operator === '/' || hasDivision(node.left) || hasDivision(node.right)
  }
  if (node.kind === 'unary') return hasDivision(node.child)
  return false
}
