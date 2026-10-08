@forelse ($logs as $log)
    <article data-log-entry data-id="{{ $log->id }}"
             class="note-enter rounded-lg px-2 py-2 transition hover:bg-stone-50 dark:hover:bg-stone-800/50" style="--i: {{ $loop->index }}">
        <div class="flex items-center gap-2">
            @if ($log->status === 'error')
                <x-phosphor-x-circle-fill class="size-3.5 shrink-0 text-red-500" />
            @elseif ($log->status === 'dry-run')
                <x-phosphor-eye class="size-3.5 shrink-0 text-accent-600 dark:text-accent-400" />
            @else
                <x-phosphor-check-circle-fill class="size-3.5 shrink-0 text-emerald-500" />
            @endif
            <code class="min-w-0 flex-1 truncate text-xs font-medium" title="{{ $log->command }}">{{ $log->command }}</code>
            <span class="shrink-0 rounded bg-stone-100 px-1 py-0.5 font-mono text-[10px] text-stone-400 dark:bg-stone-800">{{ $log->scope }}</span>
        </div>
        <div class="mt-0.5 flex items-center justify-between pl-5">
            <time class="text-[10px] text-stone-400">{{ $log->created_at->diffForHumans() }}</time>
        </div>
        @if (trim($log->output ?? '') !== '')
            <details class="mt-1 pl-5">
                <summary class="cursor-pointer select-none list-none text-[11px] text-stone-400 transition hover:text-stone-600 dark:hover:text-stone-300 [&::-webkit-details-marker]:hidden">
                    output ▸
                </summary>
                <pre class="mt-1 max-h-48 overflow-auto rounded-md bg-stone-100 p-2 font-mono text-[10px] leading-relaxed text-stone-600 dark:bg-stone-800 dark:text-stone-300">{{ $log->output }}</pre>
            </details>
        @endif
    </article>
@empty
    <div class="flex flex-col items-center gap-2 px-3 py-10 text-center">
        <x-phosphor-terminal class="size-8 text-stone-300 dark:text-stone-600" />
        <p class="text-xs text-stone-400">No commands run yet.<br>Actions show up here.</p>
    </div>
@endforelse
