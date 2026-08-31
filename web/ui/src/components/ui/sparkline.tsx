import { useMemo } from "react"

interface SparklineProps {
  values: number[]
  /** Kosongkan untuk skala otomatis (default untuk network throughput). */
  max?: number
  color?: string
  height?: number
  className?: string
}

// SVG inline. Tidak pakai library chart — satu area line per metrik,
// bundle hemat ~200KB vs ECharts/Chart.js. PRD §4.2 allowlist.

const W = 200

export function Sparkline({
  values,
  max,
  color = "var(--color-signal)",
  height = 36,
  className,
}: SparklineProps) {
  const geom = useMemo(() => {
    const v = values
    if (v.length < 2) return { line: "", area: "" }
    const peak = max ?? Math.max(...v, 1)
    const H = height
    const step = W / (v.length - 1)
    const pts = v.map((n, i) => ({
      x: i * step,
      y: H - (Math.min(n, peak) / peak) * (H - 2) - 1,
    }))
    // Kurva halus: tiap titik jadi control point, kurva ditarik ke titik
    // tengah antar-titik. Menghilangkan sudut patah pada polyline tanpa
    // library chart — dan tidak pernah melewati batas nilai seperti
    // Catmull-Rom (overshoot) karena hanya kuadratik.
    let line = `M${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)}`
    for (let i = 1; i < pts.length - 1; i++) {
      const mx = (pts[i].x + pts[i + 1].x) / 2
      const my = (pts[i].y + pts[i + 1].y) / 2
      line += ` Q${pts[i].x.toFixed(1)},${pts[i].y.toFixed(1)} ${mx.toFixed(1)},${my.toFixed(1)}`
    }
    const last = pts[pts.length - 1]
    line += ` L${last.x.toFixed(1)},${last.y.toFixed(1)}`
    return { line, area: `${line} L${W},${H} L0,${H} Z` }
  }, [values, max, height])

  return (
    <svg
      viewBox={`0 0 ${W} ${height}`}
      height={height}
      preserveAspectRatio="none"
      className={`w-full ${className ?? ""}`}
      role="img"
      aria-hidden
    >
      <path d={geom.area} fill={color} fillOpacity={0.12} />
      <path
        d={geom.line}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}