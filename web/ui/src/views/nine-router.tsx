import { useT } from "@/stores/i18n"

const NINE_ROUTER_URL = "http://100.89.81.126:20128/"

export function NineRouterView() {
  const t = useT()
  // ponytail: endpoint tetap; reverse proxy HTTPS diperlukan untuk panel HTTPS.
  if (window.location.protocol === "https:") {
    return <p role="alert" className="rounded-lg border border-border p-4 text-sm">{t("nineRouter.mixedContent")}</p>
  }
  return (
    <iframe
      src={NINE_ROUTER_URL}
      title="9Router"
      className="h-[calc(100dvh-9rem)] min-h-96 w-full rounded-lg border border-border bg-white"
      referrerPolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"
    />
  )
}
