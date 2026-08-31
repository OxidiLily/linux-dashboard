import * as React from "react"
import { cn } from "@/lib/utils"

// <label> bawaan HTML: htmlFor sudah menghubungkannya ke input, dan klik pada
// label sudah memfokuskan input itu tanpa JavaScript apa pun.
export const Label = React.forwardRef<HTMLLabelElement, React.LabelHTMLAttributes<HTMLLabelElement>>(
  ({ className, ...props }, ref) => (
    <label
      ref={ref}
      className={cn("text-xs font-medium text-foreground peer-disabled:opacity-50", className)}
      {...props}
    />
  ),
)
Label.displayName = "Label"
