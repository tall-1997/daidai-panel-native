export interface GeeTestValidateResult {
  lot_number: string
  captcha_output: string
  pass_token: string
  gen_time: string
}

interface GeeTestCaptchaObject {
  showCaptcha: () => void
  reset?: () => void
  getValidate: () => GeeTestValidateResult | undefined
  onReady: (callback: () => void) => GeeTestCaptchaObject
  onSuccess: (callback: () => void) => GeeTestCaptchaObject
  onError: (callback: (error?: unknown) => void) => GeeTestCaptchaObject
  onClose?: (callback: () => void) => GeeTestCaptchaObject
}

interface GeeTestWindow extends Window {
  initGeetest4?: (
    config: {
      captchaId: string
      https?: boolean
      product?: string
      language?: string
    },
    callback: (captchaObj: GeeTestCaptchaObject) => void
  ) => void
}

export interface GeeTestInstance {
  show: () => void
  reset: () => void
  getResult: () => GeeTestValidateResult | null
}

export interface GeeTestInstanceOptions {
  captchaId: string
  language?: string
  product?: string
}

export interface GeeTestInstanceHandlers {
  onReady?: () => void
  onSuccess?: (result: GeeTestValidateResult) => void
  onError?: (error: Error) => void
  onClose?: () => void
}

const geetestSdkUrl = 'https://static.geetest.com/v4/gt4.js'
let sdkPromise: Promise<void> | null = null

export async function ensureGeeTestSdk() {
  if (typeof window === 'undefined') {
    throw new Error('当前环境不支持验证码 SDK')
  }

  const geetestWindow = window as GeeTestWindow
  if (typeof geetestWindow.initGeetest4 === 'function') {
    return
  }

  if (!sdkPromise) {
    sdkPromise = new Promise<void>((resolve, reject) => {
      const existing = document.querySelector<HTMLScriptElement>(`script[src="${geetestSdkUrl}"]`)
      if (existing) {
        existing.addEventListener('load', () => resolve(), { once: true })
        existing.addEventListener('error', () => {
          sdkPromise = null
          reject(new Error('极验 SDK 加载失败'))
        }, { once: true })
        return
      }

      const script = document.createElement('script')
      script.src = geetestSdkUrl
      script.async = true
      script.onload = () => resolve()
      script.onerror = () => {
        sdkPromise = null
        reject(new Error('极验 SDK 加载失败'))
      }
      document.head.appendChild(script)
    })
  }

  await sdkPromise
}

export async function createGeeTestInstance(
  options: GeeTestInstanceOptions,
  handlers: GeeTestInstanceHandlers = {}
): Promise<GeeTestInstance> {
  await ensureGeeTestSdk()

  const geetestWindow = window as GeeTestWindow
  if (typeof geetestWindow.initGeetest4 !== 'function') {
    throw new Error('极验 SDK 未初始化')
  }

  return new Promise<GeeTestInstance>((resolve, reject) => {
    try {
      geetestWindow.initGeetest4!(
        {
          captchaId: options.captchaId,
          https: true,
          product: options.product || 'bind',
          language: options.language || 'zho'
        },
        (captchaObj) => {
          const instance: GeeTestInstance = {
            show: () => {
              captchaObj.showCaptcha()
            },
            reset: () => {
              captchaObj.reset?.()
            },
            getResult: () => captchaObj.getValidate() || null
          }

          captchaObj
            .onReady(() => {
              handlers.onReady?.()
              resolve(instance)
            })
            .onSuccess(() => {
              const result = captchaObj.getValidate()
              if (!result) {
                handlers.onError?.(new Error('验证码结果为空'))
                return
              }
              handlers.onSuccess?.(result)
            })
            .onError((error) => {
              handlers.onError?.(toError(error))
            })

          captchaObj.onClose?.(() => {
            handlers.onClose?.()
          })
        }
      )
    } catch (error) {
      reject(toError(error))
    }
  })
}

function toError(error: unknown) {
  if (error instanceof Error) {
    return error
  }

  if (typeof error === 'string') {
    return new Error(error)
  }

  return new Error('验证码初始化失败')
}
