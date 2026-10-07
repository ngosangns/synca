@props(['q' => ''])

<div class="col-span-full flex flex-col items-center gap-3 py-20 text-center">
    <span class="flex size-14 items-center justify-center rounded-2xl bg-stone-200/70 text-stone-400 dark:bg-stone-800 dark:text-stone-500">
        @if ($q !== '')
            <x-phosphor-magnifying-glass class="size-7" />
        @else
            <x-phosphor-note-blank class="size-7" />
        @endif
    </span>
    @if ($q !== '')
        <p class="text-sm text-stone-500 dark:text-stone-400">No notes match <span class="font-medium text-stone-700 dark:text-stone-200">&ldquo;{{ $q }}&rdquo;</span>.</p>
        <a href="{{ route('notes.index') }}" data-clear-search
           class="text-sm font-medium text-accent-600 transition hover:text-accent-500 dark:text-accent-400">Clear search</a>
    @else
        <p class="text-sm text-stone-500 dark:text-stone-400">No notes yet. Write your first one above.</p>
    @endif
</div>
