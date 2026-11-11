'use client';
import { Check, Copy } from 'lucide-react';
import { useState } from 'react';

export function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex w-full max-w-3xl min-w-0 items-center gap-3 rounded-lg border bg-fd-card px-4 py-3 text-left font-mono text-sm shadow-sm">
      <span className="select-none text-fd-muted-foreground">$</span>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap">{command}</code>
      <button
        type="button"
        aria-label="Copy the install command"
        className="rounded-md p-1.5 text-fd-muted-foreground transition-colors hover:bg-fd-accent hover:text-fd-foreground"
        onClick={() => {
          navigator.clipboard.writeText(command).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
}
