import { useState, type KeyboardEvent } from "react";
import { QRService } from "../bindings/neutron-star";

function App() {
  const [text, setText] = useState<string>('')
  const [qrSrc, setQrSrc] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [generating, setGenerating] = useState<boolean>(false)

  const handleGenerate = async (): Promise<void> => {
    const trimmed = text.trim()
    if (!trimmed) {
      setError('请输入要编码的文本')
      return
    }

    setGenerating(true)
    setError(null)
    try {
      const qrCodeBase64 = await QRService.Generate(trimmed, 256)
      if (qrCodeBase64 === null) {
        setError('生成失败：Go 后端返回了 null')
        return
      }
      setQrSrc(`data:image/png;base64,${qrCodeBase64}`)
    } catch (err) {
      setError('生成失败：' + (err instanceof Error ? err.message : String(err)))
    } finally {
      setGenerating(false)
    }
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>): void => {
    if (e.key === 'Enter') {
      void handleGenerate()
    }
  }

  return (
    <main className="container">
      <h1 className="title">
        <span className="title-accent">QR</span> Genertor
      </h1>
      <p className="subtitle">输入文本，点击按钮生成二维码</p>

      <div className="greet">
        <div className="input">
          <input 
            aria-label="text"
            className="input"
            type="test"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Enter text to encode"
          />
          <button>
            {generating ? 'Generating...' : 'Generated'}
          </button>
        </div>
      </div>
      {
        error && (
          <p className="subtitle" role="alert">
            {error}
          </p>
        )
      }

      {qrSrc && (
        <img
          className="qr-img"
          src={qrSrc}
          alt="Generated QR code"
          width={256}
          height={256}
        />
      )}
    </main>
  )
}

export default App