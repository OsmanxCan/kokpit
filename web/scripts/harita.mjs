// @svg-maps/world haritasını küçültür (koordinatlar 0,1 hassasiyete yuvarlanır) ve
// src/lib/world-map.json olarak yazar. Harita: © Victor Cazanave, CC BY 4.0.
// Kullanım: node scripts/harita.mjs
import fs from 'node:fs'
import world from '@svg-maps/world'

const r = (v) => Math.round(v * 10) / 10
const f = (v) => {
  const s = (Math.round(v * 10) / 10).toFixed(1)
  return s.endsWith('.0') ? s.slice(0, -2) : s
}

// Mesafesi TOL'den küçük ardışık noktaları atar; ekranda fark edilmez, dosyayı çok küçültür.
const TOL = 0.6
const n = (v) => f(v).replace(/^(-?)0\./, '$1.')

function shrink(d) {
  const tok = d.match(/[mMzZ]|-?(?:\d+\.?\d*|\.\d+)(?:e-?\d+)?/g)
  const rings = []
  let cur = null, cx = 0, cy = 0, sx = 0, sy = 0, i = 0, newSub = false
  while (i < tok.length) {
    const t = tok[i]
    if (t === 'z' || t === 'Z') { cx = sx; cy = sy; cur = null; i++; continue }
    if (t === 'm') { newSub = true; i++; continue }
    cx += parseFloat(t); cy += parseFloat(tok[i + 1]); i += 2
    if (newSub || !cur) { cur = []; rings.push(cur); sx = cx; sy = cy; newSub = false }
    cur.push([cx, cy])
  }
  let out = ''
  for (const ring of rings) {
    const kept = [ring[0]]
    for (const p of ring.slice(1)) {
      const q = kept[kept.length - 1]
      if (Math.hypot(p[0] - q[0], p[1] - q[1]) >= TOL) kept.push(p)
    }
    if (kept.length < 3) {
      // küçük ada: en azından bir üçgen kalsın
      if (ring.length < 3) continue
      kept.splice(0, kept.length, ring[0], ring[Math.floor(ring.length / 3)], ring[Math.floor((2 * ring.length) / 3)])
    }
    let px = r(kept[0][0]), py = r(kept[0][1])
    let seg = `M${n(px)} ${n(py)}l`
    const parts = []
    for (const p of kept.slice(1)) {
      const ax = r(p[0]), ay = r(p[1])
      parts.push(`${n(ax - px)} ${n(ay - py)}`)
      px = ax; py = ay
    }
    out += seg + parts.join(' ').replace(/ -/g, '-') + 'z'
  }
  return out
}

const locs = world.locations.map((l) => ({ id: l.id, d: shrink(l.path) }))
const json = JSON.stringify({ viewBox: world.viewBox, locations: locs })
fs.writeFileSync(new URL('../src/lib/world-map.json', import.meta.url), json)
console.log(`${locs.length} ülke, ${(json.length / 1024).toFixed(0)} KB`)
