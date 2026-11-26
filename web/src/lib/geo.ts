// Ülke adları (Türkçe) ve bayrak emojileri.
let regionNames: Intl.DisplayNames | null = null
try { regionNames = new Intl.DisplayNames(['tr'], { type: 'region' }) } catch { /* eski tarayıcı */ }
export const countryName = (cc: string) => {
  if (!cc || cc === '??') return 'Bilinmiyor'
  try { return regionNames?.of(cc) ?? cc } catch { return cc }
}
export const flag = (cc: string) => (cc && cc.length === 2 && cc !== '??')
  ? String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1a5 + c.charCodeAt(0)))
  : '🏳️'

