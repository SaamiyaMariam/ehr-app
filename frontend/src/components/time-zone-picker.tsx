"use client";

import { KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { TIME_ZONES } from "@/lib/time-zones";

function cityLabel(timeZone: string) {
  if (timeZone === "UTC") return "UTC — Coordinated Universal Time";
  const parts = timeZone.replaceAll("_", " ").split("/");
  const city = parts.pop();
  return parts.length ? `${city} (${parts.join(" / ")})` : timeZone;
}

const cityOptions = TIME_ZONES.map((value) => ({ value, label: cityLabel(value) }))
  .sort((a, b) => a.label.localeCompare(b.label));

type Props = {
  id: string;
  value: string;
  onChange: (value: string) => void;
  className: string;
};

export default function TimeZonePicker({ id, value, onChange, className }: Props) {
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const options = value && !TIME_ZONES.includes(value)
    ? [{ value, label: cityLabel(value) }, ...cityOptions]
    : cityOptions;
  const search = query.trim().toLowerCase().replaceAll("_", " ");
  const filtered = options.filter((option) =>
    `${option.label} ${option.value.replaceAll("_", " ")}`.toLowerCase().includes(search),
  );

  useEffect(() => {
    if (open) {
      listRef.current?.querySelector(`[data-index="${active}"]`)
        ?.scrollIntoView({ block: "nearest" });
    }
  }, [active, open, query]);

  function choose(timeZone: string) {
    onChange(timeZone);
    setOpen(false);
    setQuery("");
    inputRef.current?.setCustomValidity("");
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      setActive(open
        ? Math.max(0, Math.min(filtered.length - 1, active + (event.key === "ArrowDown" ? 1 : -1)))
        : 0);
    } else if (event.key === "Enter" && open) {
      event.preventDefault();
      if (filtered[active]) choose(filtered[active].value);
    } else if (event.key === "Escape" && open) {
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
      setQuery("");
      inputRef.current?.setCustomValidity("");
    }
  }

  return (
    <div className="relative">
      <input type="hidden" name="time_zone" value={value} />
      <input
        ref={inputRef}
        id={id}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open && filtered[active] ? `${listId}-${active}` : undefined}
        autoComplete="off"
        placeholder="Search by city, e.g. Karachi"
        className={`${className} pr-16 placeholder:text-slate-500`}
        value={open ? query : value ? cityLabel(value) : ""}
        onFocus={() => { setOpen(true); setActive(0); setQuery(""); }}
        onClick={() => {
          if (!open) { setOpen(true); setActive(0); setQuery(""); }
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          setActive(0);
          setOpen(true);
          event.target.setCustomValidity(event.target.value ? "Select a city from the timezone list." : "");
          if (!event.target.value) onChange("");
        }}
        onBlur={() => {
          setOpen(false);
          setQuery("");
          inputRef.current?.setCustomValidity("");
        }}
        onKeyDown={handleKeyDown}
      />
      <div className="absolute inset-y-0 right-3 flex items-center gap-2">
        {value && (
          <button
            type="button"
            aria-label="Clear time zone"
            className="text-slate-600 hover:text-slate-900"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => choose("")}
          >
            ×
          </button>
        )}
        <span aria-hidden="true" className="pointer-events-none text-slate-600">▾</span>
      </div>
      {open && (
        <ul
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label="Cities and time zones"
          className="absolute z-20 mt-1 max-h-60 w-full overflow-y-auto rounded-lg border border-slate-300 bg-white py-1 shadow-lg"
        >
          {filtered.map((option, index) => (
            <li key={option.value} role="presentation">
              <button
                type="button"
                role="option"
                id={`${listId}-${index}`}
                aria-selected={value === option.value}
                tabIndex={-1}
                data-index={index}
                className={`block w-full px-3 py-2 text-left text-sm ${index === active ? "bg-blue-100 text-blue-900" : "text-slate-900 hover:bg-slate-100"}`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(option.value)}
              >
                {option.label}
              </button>
            </li>
          ))}
          {!filtered.length && (
            <li role="presentation" className="px-3 py-2 text-sm text-slate-600">
              <span role="status">No matching cities. Try another city or region.</span>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
