@if ($q !== '')
    <p id="result-count" class="mb-4 text-sm text-stone-500 dark:text-stone-400">
        {{ $notes->total() }} {{ Str::plural('result', $notes->total()) }} for <span class="font-medium text-stone-700 dark:text-stone-200">&ldquo;{{ $q }}&rdquo;</span>
    </p>
@endif

<div id="notes" class="columns-1 gap-4 sm:columns-2 xl:columns-3">
    @forelse ($notes as $note)
        <x-note-card :note="$note" :index="$loop->index" />
    @empty
        <x-empty-state :q="$q" />
    @endforelse
</div>

@if ($notes->hasPages())
    <nav data-pagination class="mt-6" aria-label="Notes pages">{{ $notes->links() }}</nav>
@endif

<div id="notes-meta" data-next-url="{{ $notes->hasMorePages() ? $notes->nextPageUrl() : '' }}" hidden></div>
<div id="notes-sentinel" class="h-px" aria-hidden="true"></div>
