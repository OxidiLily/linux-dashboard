
export type SessionUser = {
  username: string
  sudo: boolean
  home: string
  shell?: string
  uid?: number
  groups?: string[]
  /** true kalau `chage -l` melaporkan password wajib diganti. */
  must_change_password?: boolean
}

export type PlatformInfo = {
  os: string
  os_version: string
  pretty_name: string
  kernel_version: string
  kernel_mode: "own" | "shared" | "none"
  platform_type: string
  hypervisor: string
  arch: string
  display: string
  kernel_note?: string
}

export type TerminalCapacity = { active: number; max: number; cores: number; login_users?: number }

export type SystemInfo = {
  hostname: string
  server_time: string
  platform: PlatformInfo
  cores: number
  terminal: TerminalCapacity
}

export type CpuInfo = {
  total_pct: number
  per_core: number[]
  cores: number
  model: string
  load1: number
  load5: number
  load15: number
}

export type MemoryInfo = {
  total: number
  used: number
  available: number
  cached: number
  used_pct: number
  swap_total: number
  swap_used: number
  swap_pct: number
}

export type DiskInfo = {
  mount: string
  device: string
  fstype: string
  total: number
  used: number
  free: number
  used_pct: number
}

/** Disk terpasang yang belum dipakai — belum di-mount, tanpa partisi/LVM. */
export type UnusedDisk = {
  name: string
  path: string
  size: number
  model?: string
}

export type GpuInfo = {
  vendor: string
  name: string
  utilization_pct: number
  mem_used_mb: number
  mem_total_mb: number
  temp_c?: number
}

export type NetInfo = {
  name: string
  rx_bytes: number
  tx_bytes: number
  rx_rate: number
  tx_rate: number
}

export type Snapshot = {
  timestamp: number
  uptime: number
  cpu: CpuInfo
  memory: MemoryInfo
  disks: DiskInfo[]
  unused_disks: UnusedDisk[]
  gpus: GpuInfo[]
  network: NetInfo[]
  processes: number
}

export type Threshold = { metric: string; warn_pct: number; crit_pct: number }

export type AlertLevel = "idle" | "warn" | "crit"

export type ApiError = { error: string }
