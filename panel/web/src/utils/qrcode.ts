export async function createQrCodeDataUrl(text: string, width = 220) {
  const { toDataURL } = await import('qrcode')
  return toDataURL(text, { width, margin: 2 })
}
