/**
 * LSP WebSocket Client — gopls 자동완성/진단/hover 연동
 *
 * WebSocket으로 gopls LSP 서버에 연결하여 JSON-RPC 2.0 메시지를 주고받음.
 * Monaco Editor의 CompletionProvider, HoverProvider 등에 연결하여 사용.
 */

type LSPNotificationHandler = (method: string, params: unknown) => void

interface PendingRequest {
  resolve: (result: unknown) => void
  reject: (error: unknown) => void
}

export class LSPClient {
  private ws: WebSocket | null = null
  private requestId = 0
  private pendingRequests = new Map<number, PendingRequest>()
  private initialized = false
  private onNotification: LSPNotificationHandler | null = null
  private onDiagnostics: ((diagnostics: LSPDiagnostic[]) => void) | null = null
  private documentUri = ''
  private documentVersion = 0

  constructor(
    private url: string,
    private sessionId: string
  ) {}

  /** WebSocket 연결 + LSP initialize */
  async connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const wsUrl = `${this.url}?session_id=${this.sessionId}`
      this.ws = new WebSocket(wsUrl)

      this.ws.onopen = async () => {
        try {
          await this.initialize()
          resolve()
        } catch (err) {
          reject(err)
        }
      }

      this.ws.onmessage = (event) => {
        this.handleMessage(event.data)
      }

      this.ws.onerror = () => {
        reject(new Error('WebSocket connection failed'))
      }

      this.ws.onclose = () => {
        this.initialized = false
        this.pendingRequests.forEach((p) => p.reject(new Error('Connection closed')))
        this.pendingRequests.clear()
      }
    })
  }

  /** 연결 해제 */
  disconnect() {
    if (this.ws) {
      // LSP shutdown + exit
      this.sendRequest('shutdown', null).catch(() => {})
      setTimeout(() => {
        this.sendNotification('exit', null)
        this.ws?.close()
        this.ws = null
      }, 100)
    }
  }

  /** 알림 핸들러 등록 (diagnostics 등) */
  setNotificationHandler(handler: LSPNotificationHandler) {
    this.onNotification = handler
  }

  /** diagnostics 핸들러 등록 */
  setDiagnosticsHandler(handler: (diagnostics: LSPDiagnostic[]) => void) {
    this.onDiagnostics = handler
  }

  /** 문서 열기 (didOpen) */
  async openDocument(uri: string, content: string) {
    this.documentUri = uri
    this.documentVersion = 1
    this.sendNotification('textDocument/didOpen', {
      textDocument: {
        uri,
        languageId: 'go',
        version: this.documentVersion,
        text: content,
      },
    })
  }

  /** 문서 변경 (didChange) */
  async changeDocument(content: string) {
    this.documentVersion++
    this.sendNotification('textDocument/didChange', {
      textDocument: {
        uri: this.documentUri,
        version: this.documentVersion,
      },
      contentChanges: [{ text: content }],
    })
  }

  /** 자동완성 요청 */
  async completion(line: number, character: number): Promise<LSPCompletionItem[]> {
    const result = await this.sendRequest('textDocument/completion', {
      textDocument: { uri: this.documentUri },
      position: { line, character },
    }) as LSPCompletionResponse | null

    if (!result) return []
    const items = Array.isArray(result) ? result : result.items || []
    return items
  }

  /** Hover 요청 */
  async hover(line: number, character: number): Promise<LSPHoverResult | null> {
    const result = await this.sendRequest('textDocument/hover', {
      textDocument: { uri: this.documentUri },
      position: { line, character },
    }) as LSPHoverResult | null

    return result
  }

  /** Signature Help 요청 */
  async signatureHelp(line: number, character: number): Promise<LSPSignatureHelp | null> {
    return await this.sendRequest('textDocument/signatureHelp', {
      textDocument: { uri: this.documentUri },
      position: { line, character },
    }) as LSPSignatureHelp | null
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN && this.initialized
  }

  // --- Internal ---

  private async initialize(): Promise<void> {
    const result = await this.sendRequest('initialize', {
      processId: null,
      rootUri: null,
      capabilities: {
        textDocument: {
          completion: {
            completionItem: {
              snippetSupport: true,
              commitCharactersSupport: true,
              documentationFormat: ['markdown', 'plaintext'],
            },
          },
          hover: {
            contentFormat: ['markdown', 'plaintext'],
          },
          signatureHelp: {
            signatureInformation: {
              documentationFormat: ['markdown', 'plaintext'],
            },
          },
          publishDiagnostics: {
            relatedInformation: true,
          },
        },
      },
    })

    if (result) {
      this.initialized = true
      this.sendNotification('initialized', {})
    }
  }

  private sendRequest(method: string, params: unknown): Promise<unknown> {
    return new Promise((resolve, reject) => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
        reject(new Error('Not connected'))
        return
      }

      const id = ++this.requestId
      this.pendingRequests.set(id, { resolve, reject })

      const message = JSON.stringify({
        jsonrpc: '2.0',
        id,
        method,
        params,
      })

      this.ws.send(message)

      // 10초 타임아웃
      setTimeout(() => {
        if (this.pendingRequests.has(id)) {
          this.pendingRequests.delete(id)
          reject(new Error(`LSP request timeout: ${method}`))
        }
      }, 10000)
    })
  }

  private sendNotification(method: string, params: unknown) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return

    const message = JSON.stringify({
      jsonrpc: '2.0',
      method,
      params,
    })

    this.ws.send(message)
  }

  private handleMessage(data: string) {
    try {
      const msg = JSON.parse(data) as LSPMessage

      // Response (id가 있으면)
      if ('id' in msg && msg.id !== undefined) {
        const pending = this.pendingRequests.get(msg.id as number)
        if (pending) {
          this.pendingRequests.delete(msg.id as number)
          if ('error' in msg) {
            pending.reject(msg.error)
          } else {
            pending.resolve(msg.result)
          }
        }
        return
      }

      // Notification (id 없음)
      if ('method' in msg) {
        if (msg.method === 'textDocument/publishDiagnostics' && this.onDiagnostics) {
          const params = msg.params as { diagnostics: LSPDiagnostic[] }
          this.onDiagnostics(params.diagnostics || [])
        }
        this.onNotification?.(msg.method as string, msg.params)
      }
    } catch {
      // 파싱 실패 무시
    }
  }
}

// --- LSP Types ---

interface LSPMessage {
  jsonrpc: string
  id?: number
  method?: string
  result?: unknown
  error?: unknown
  params?: unknown
}

export interface LSPCompletionItem {
  label: string
  kind?: number
  detail?: string
  documentation?: string | { kind: string; value: string }
  insertText?: string
  insertTextFormat?: number
  filterText?: string
  sortText?: string
  textEdit?: {
    range: LSPRange
    newText: string
  }
}

interface LSPCompletionResponse {
  isIncomplete?: boolean
  items: LSPCompletionItem[]
}

export interface LSPHoverResult {
  contents: string | { kind: string; value: string } | Array<string | { kind: string; value: string }>
  range?: LSPRange
}

export interface LSPSignatureHelp {
  signatures: Array<{
    label: string
    documentation?: string | { kind: string; value: string }
    parameters?: Array<{
      label: string | [number, number]
      documentation?: string
    }>
  }>
  activeSignature?: number
  activeParameter?: number
}

export interface LSPDiagnostic {
  range: LSPRange
  severity?: number // 1=Error, 2=Warning, 3=Info, 4=Hint
  message: string
  source?: string
}

interface LSPRange {
  start: { line: number; character: number }
  end: { line: number; character: number }
}

/** LSP CompletionItemKind → Monaco CompletionItemKind 변환 */
export function lspKindToMonaco(kind?: number): number {
  // LSP spec: 1=Text, 2=Method, 3=Function, 4=Constructor, 5=Field, 6=Variable, 7=Class, 8=Interface, 9=Module, 10=Property, 11=Unit, 12=Value, 13=Enum, 14=Keyword, 15=Snippet, 16=Color, 17=File, 18=Reference, 19=Folder, 20=EnumMember, 21=Constant, 22=Struct, 23=Event, 24=Operator, 25=TypeParameter
  // Monaco: 같은 numbering 사용
  return kind || 18 // default: Reference
}

/** LSP Diagnostic severity → Monaco MarkerSeverity 변환 */
export function lspSeverityToMonaco(severity?: number): number {
  // LSP: 1=Error, 2=Warning, 3=Info, 4=Hint
  // Monaco MarkerSeverity: 8=Error, 4=Warning, 2=Info, 1=Hint
  switch (severity) {
    case 1: return 8  // Error
    case 2: return 4  // Warning
    case 3: return 2  // Info
    case 4: return 1  // Hint
    default: return 2 // Info
  }
}
