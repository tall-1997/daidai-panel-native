const scriptFilePattern = /\.(?:js|ts|py|sh|go)$/i

export interface TaskCommandDisplayParts {
  before: string
  script: string | null
  after: string
}

function tokenizeCommand(command: string) {
  const tokens: string[] = []
  let current = ''
  let quote: '"' | "'" | null = null

  for (let i = 0; i < command.length; i += 1) {
    const char = command[i] ?? ''
    if (quote) {
      if (char === quote) {
        quote = null
        continue
      }
      current += char
      continue
    }

    if (char === '"' || char === "'") {
      quote = char
      continue
    }

    if (/\s/.test(char)) {
      if (current) {
        tokens.push(current)
        current = ''
      }
      continue
    }

    current += char
  }

  if (current) {
    tokens.push(current)
  }

  return tokens
}

function firstScriptToken(tokens: string[]) {
  return tokens.find(token => scriptFilePattern.test(token)) || null
}

export function extractTaskCommandScriptPath(command: string) {
  const tokens = tokenizeCommand(command)
  if (tokens.length === 0) return null

  const entry = tokens[0]
  if (!entry) return null
  const rest = tokens.slice(1)
  const normalizedEntry = entry.toLowerCase()

  if (normalizedEntry === 'task') {
    for (let i = 0; i < rest.length; i += 1) {
      const token = rest[i]
      if (!token) continue
      if (token === '--') break
      if (token === '-m') {
        i += 1
        continue
      }
      if (scriptFilePattern.test(token)) {
        return token
      }
    }
    return null
  }

  if (['desi', 'node', 'nodejs', 'python', 'python3', 'bash', 'sh', 'ts-node', 'go'].includes(normalizedEntry)) {
    return firstScriptToken(rest)
  }

  return firstScriptToken(tokens)
}

export function splitTaskCommandDisplay(command: string): TaskCommandDisplayParts {
  const script = extractTaskCommandScriptPath(command)
  if (!script) {
    return {
      before: command,
      script: null,
      after: '',
    }
  }

  const scriptIndex = command.indexOf(script)
  if (scriptIndex < 0) {
    return {
      before: command,
      script: null,
      after: '',
    }
  }

  return {
    before: command.slice(0, scriptIndex),
    script,
    after: command.slice(scriptIndex + script.length),
  }
}
