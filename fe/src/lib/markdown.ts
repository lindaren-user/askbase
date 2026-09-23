import { marked } from 'marked'
import DOMPurify from 'dompurify'
import katex from 'katex'

marked.setOptions({
  gfm: true,
  breaks: true,
})

export function renderMarkdown(content: string): string {
  const extracted = extractMath(content || '')
  let html = marked.parse(extracted.markdown, { async: false }) as string
  for (const token of extracted.tokens) {
    html = html.replaceAll(token.placeholder, token.html)
  }
  const clean = DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true, mathMl: true },
    ADD_DATA_URI_TAGS: ['img'],
  })
  return wrapMarkdownTables(clean)
}

interface MathDelimiter {
  left: string
  right: string
  display: boolean
}

interface RenderedMath {
  placeholder: string
  html: string
}

interface ExtractedMath {
  markdown: string
  tokens: RenderedMath[]
}

interface MathOpening {
  index: number
  delimiter: MathDelimiter
}

const mathDelimiters: MathDelimiter[] = [
  { left: '$$', right: '$$', display: true },
  { left: '\\\\[', right: '\\\\]', display: true },
  { left: '\\[', right: '\\]', display: true },
  { left: '\\\\(', right: '\\\\)', display: false },
  { left: '\\(', right: '\\)', display: false },
  { left: '$', right: '$', display: false },
]

/** 在 Markdown 解析前提取公式，确保原始 HTML 表格单元格里的公式也能渲染。 */
function extractMath(markdown: string): ExtractedMath {
  const protectedCode: string[] = []
  const source = markdown.replace(
    /(```[\s\S]*?```|~~~[\s\S]*?~~~|<pre\b[\s\S]*?<\/pre>|<code\b[\s\S]*?<\/code>|`+[^`\n]*`+)/gi,
    (code) => {
      const placeholder = `ASKBASECODETOKEN${protectedCode.length}END`
      protectedCode.push(code)
      return placeholder
    },
  )
  const tokens: RenderedMath[] = []
  let output = ''
  let cursor = 0

  while (cursor < source.length) {
    const opening = findMathOpening(source, cursor)
    if (!opening) {
      output += source.slice(cursor)
      break
    }
    output += source.slice(cursor, opening.index)
    const formulaStart = opening.index + opening.delimiter.left.length
    const formulaEnd = findMathEnd(source, formulaStart, opening.delimiter.right)
    if (formulaEnd < 0) {
      output += source.slice(opening.index)
      break
    }
    if (opening.delimiter.display) {
      output = output.replace(/\[公式\]\s*$/, '')
    }
    const formula = source.slice(formulaStart, formulaEnd).trim()
    const placeholder = `ASKBASEMATHTOKEN${tokens.length}END`
    tokens.push({
      placeholder,
      html: katex.renderToString(formula, {
        displayMode: opening.delimiter.display,
        throwOnError: false,
        trust: false,
      }),
    })
    output += placeholder
    cursor = formulaEnd + opening.delimiter.right.length
  }

  for (const [index, code] of protectedCode.entries()) {
    output = output.replaceAll(`ASKBASECODETOKEN${index}END`, code)
  }
  return { markdown: output, tokens }
}

function findMathOpening(source: string, start: number): MathOpening | null {
  for (let index = start; index < source.length; index++) {
    if (isEscaped(source, index)) continue
    for (const delimiter of mathDelimiters) {
      if (source.startsWith(delimiter.left, index)) return { index, delimiter }
    }
  }
  return null
}

function findMathEnd(source: string, start: number, delimiter: string): number {
  let braceDepth = 0
  for (let index = start; index < source.length; index++) {
    if (braceDepth <= 0 && source.startsWith(delimiter, index)) return index
    if (source[index] === '\\') {
      index++
    } else if (source[index] === '{') {
      braceDepth++
    } else if (source[index] === '}') {
      braceDepth--
    }
  }
  return -1
}

function isEscaped(source: string, index: number): boolean {
  let slashes = 0
  for (let cursor = index - 1; cursor >= 0 && source[cursor] === '\\'; cursor--) slashes++
  return slashes % 2 === 1
}

/** 为宽表增加独立滚动层，避免表格被分块卡片或页面宽度裁切。 */
function wrapMarkdownTables(html: string): string {
  const template = document.createElement('template')
  template.innerHTML = html
  for (const table of template.content.querySelectorAll('table')) {
    if (table.parentElement?.classList.contains('md-table-scroll')) continue
    const wrapper = document.createElement('div')
    wrapper.className = 'md-table-scroll'
    table.before(wrapper)
    wrapper.append(table)
  }
  return template.innerHTML
}

const citeButton =
  '<button type="button" class="cite-ref text-[0.8rem] font-medium text-blue-600 underline decoration-blue-600/35 underline-offset-2 hover:decoration-blue-600 dark:text-blue-400 dark:decoration-blue-400/40 dark:hover:decoration-blue-400" data-cite="$1">[$1]</button>'

export function renderAssistantMarkdown(content: string, citationNumbers: readonly number[] = []): string {
  const clean = renderMarkdown(content)
  if (citationNumbers.length === 0) return clean
  const validCitations = new Set(citationNumbers)
  return clean.replace(/\[(\d{1,3})\]/g, (match, num) => {
    const n = Number(num)
    if (!validCitations.has(n)) return match
    return citeButton.replaceAll('$1', String(n))
  })
}
