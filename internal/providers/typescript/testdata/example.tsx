import React, { useState } from "react"

interface Props { title: string }

export function Card({ title }: Props) {
  const [open, setOpen] = useState(false)
  return (
    <div className="card" onClick={() => setOpen(!open)}>
      <h1>{title}</h1>
      {open ? <p>details</p> : null}
    </div>
  )
}
