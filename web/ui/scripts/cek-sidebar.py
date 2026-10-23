"""Jalankan: python3 web/ui/scripts/cek-sidebar.py"""
import re
from pathlib import Path

src = Path(__file__).resolve().parents[1] / "src"
shell = (src / "components/layout/app-shell.tsx").read_text()
nav = shell.split("const NAV: NavGroup[] = [", 1)[1].split("\n]\n", 1)[0]
kamus = (src / "stores/i18n.ts").read_text()
labels = dict(re.findall(r'"(nav\.[^"]+)": \{ id: "([^"]+)"', kamus))
expected = [
    ("Overview", ["Dashboard"]),
    ("Storage & Files", ["File Manager", "Samba", "Disk Pool", "NFS Exports", "Bookmarks"]),
    ("AI & Automation", ["AI Agent", "9Router"]),
    ("Network", ["Network", "Speedtest", "Proxy Manager"]),
    ("Security", ["Firewall", "Fail2Ban"]),
    ("System Management", ["Processes", "Docker", "Cronjob", "Terminal", "Print Server", "Components"]),
    ("Logs & Monitoring", ["Logs", "File Operations Log", "Activity Logs", "Alert Thresholds"]),
]
actual = []
paths = []
for group, items in re.findall(r'group: "([^"]+)",\s*items: \[(.*?)\]', nav, re.S):
    visible = re.findall(r'\{ to: "([^"]+)", label: "([^"]+)"[^}]*\}', items)
    visible = [(path, key) for path, key in visible if not re.search(
        rf'\{{ to: "{re.escape(path)}"[^}}]*tersembunyi: true', items)]
    actual.append((labels[group], [labels[key] for _, key in visible]))
    paths.extend(path for path, _ in visible)
assert actual == expected, actual
assert len(paths) == len(set(paths)), "Duplikasi menu"
router = (src / "router/index.tsx").read_text()
for path in paths:
    assert path == "/" or f'path: "{path.lstrip(chr(47))}"' in router, path
assert '{ path: "ai/9router", element: <Dijaga name="9router" label="9Router"><NineRouterView /></Dijaga> }' in router
assert '{ path: "settings/speedtest", element: <Lazy><SpeedtestView /></Lazy> }' in router
print("PASS: urutan grup/menu, tanpa duplikasi, route menu terdaftar")
