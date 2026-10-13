import { useT } from "@/stores/i18n"

// URL dinamis: port 9Router tetap 20128, hostname ikut dari mana user
// membuka panel. Ini menghilangkan hardcode IP dan bekerja baik dari LAN
// maupun Tailscale, selama 9Router mendengarkan di 0.0.0.0.
//
// CSP frame-src: server harus diberi DASHBOARD_NINE_ROUTER_URL di
// /etc/default/linux-dashboard agar browser tidak memblokir iframe.
// Saat URL dinamis, pasang nilainya wildcard port:
//   DASHBOARD_NINE_ROUTER_URL=http://192.168.2.11:20128 http://100.89.81.126:20128
// atau gunakan satu URL yang paling sering dipakai.

function nineRouterURL(): string {
  return `http://${window.location.hostname}:20128/`
}

export function NineRouterView() {
  const t = useT()
  // Panel HTTPS tidak bisa menampilkan iframe HTTP (mixed content).
  if (window.location.protocol === "https:") {
    return <p role="alert" className="rounded-lg border border-border p-4 text-sm">{t("nineRouter.mixedContent")}</p>
  }
  const url = nineRouterURL()
  return (
    <iframe
      src={url}
      title="9Router"
      className="h-[calc(100dvh-9rem)] min-h-96 w-full rounded-lg border border-border bg-white"
      referrerPolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"
    />
  )
}
