import * as React from "react"
import { Check, ChevronDown, Search } from "lucide-react"
import { cn } from "@/lib/utils"

export type SelectOption = {
  value: string
  label?: React.ReactNode
  disabled?: boolean
}

export interface SelectProps {
  value?: string
  onChange?: (value: string) => void
  options: (string | SelectOption)[]
  placeholder?: string
  disabled?: boolean
  className?: string
  triggerClassName?: string
  searchable?: boolean
  searchPlaceholder?: string
  id?: string
  name?: string
  "aria-label"?: string
}

export function Select({
  value,
  onChange,
  options,
  placeholder = "Pilih…",
  disabled = false,
  className,
  triggerClassName,
  searchable,
  searchPlaceholder = "Cari…",
  id,
  name,
  "aria-label": ariaLabel,
}: SelectProps) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState("")
  const containerRef = React.useRef<HTMLDivElement>(null)
  const searchInputRef = React.useRef<HTMLInputElement>(null)

  const normalizedOptions: SelectOption[] = React.useMemo(() => {
    return options.map((opt) => {
      if (typeof opt === "string") return { value: opt, label: opt }
      return { ...opt, label: opt.label ?? opt.value }
    })
  }, [options])

  const selectedOption = normalizedOptions.find((o) => o.value === value)
  const isSearchable = searchable ?? normalizedOptions.length > 7

  const filteredOptions = React.useMemo(() => {
    if (!isSearchable || !search.trim()) return normalizedOptions
    const q = search.trim().toLowerCase()
    return normalizedOptions.filter((opt) => {
      const labelText = typeof opt.label === "string" ? opt.label : opt.value
      return labelText.toLowerCase().includes(q) || opt.value.toLowerCase().includes(q)
    })
  }, [normalizedOptions, search, isSearchable])

  React.useEffect(() => {
    if (!open) return
    const onMouseDown = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onMouseDown)
    document.addEventListener("keydown", onKeyDown)
    if (isSearchable) {
      setTimeout(() => searchInputRef.current?.focus(), 50)
    }
    return () => {
      document.removeEventListener("mousedown", onMouseDown)
      document.removeEventListener("keydown", onKeyDown)
    }
  }, [open, isSearchable])

  const handleSelect = (val: string) => {
    onChange?.(val)
    setOpen(false)
    setSearch("")
  }

  return (
    <div ref={containerRef} className={cn("relative w-full", className)}>
      {name && <input type="hidden" name={name} value={value ?? ""} />}
      <button
        id={id}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => {
          if (!disabled) {
            setOpen((prev) => !prev)
            setSearch("")
          }
        }}
        className={cn(
          "flex h-9 w-full items-center justify-between rounded-md border border-border bg-input px-3 py-1.5 text-xs text-foreground transition-colors hover:border-muted focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
          triggerClassName,
        )}
      >
        <span className={cn("truncate text-left", !selectedOption && "text-muted-foreground")}>
          {selectedOption ? selectedOption.label : (value || placeholder)}
        </span>
        <ChevronDown
          className={cn(
            "ml-2 size-3.5 shrink-0 text-muted-foreground transition-transform duration-200",
            open && "rotate-180",
          )}
        />
      </button>

      {open && (
        <div
          role="listbox"
          className="absolute left-0 right-0 z-50 mt-1 max-h-60 overflow-y-auto rounded-md border border-border bg-surface-2 p-1 shadow-xl focus:outline-none"
        >
          {isSearchable && (
            <div className="flex items-center gap-1.5 border-b border-border px-2 py-1.5 mb-1">
              <Search className="size-3 text-muted-foreground shrink-0" />
              <input
                ref={searchInputRef}
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={searchPlaceholder}
                className="w-full bg-transparent text-xs text-foreground placeholder:text-muted-foreground focus:outline-none"
                onClick={(e) => e.stopPropagation()}
              />
            </div>
          )}
          <div className="space-y-0.5">
            {filteredOptions.length === 0 ? (
              <div className="py-2 text-center text-xs text-muted-foreground">Tidak ada hasil</div>
            ) : (
              filteredOptions.map((opt) => {
                const isSelected = opt.value === value
                return (
                  <button
                    key={opt.value}
                    type="button"
                    role="option"
                    aria-selected={isSelected}
                    disabled={opt.disabled}
                    onClick={() => handleSelect(opt.value)}
                    className={cn(
                      "flex w-full items-center justify-between rounded px-2.5 py-1.5 text-xs text-left transition-colors",
                      isSelected
                        ? "bg-accent font-medium text-foreground"
                        : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
                      opt.disabled && "cursor-not-allowed opacity-50",
                    )}
                  >
                    <span className="truncate">{opt.label}</span>
                    {isSelected && <Check className="ml-2 size-3.5 shrink-0 text-signal" />}
                  </button>
                )
              })
            )}
          </div>
        </div>
      )}
    </div>
  )
}
