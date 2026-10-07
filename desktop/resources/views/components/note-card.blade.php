@props(['note', 'index' => 0])

<article @class([
    'note-card note-enter mb-4 break-inside-avoid rounded-xl border bg-white p-4 shadow-sm dark:bg-stone-900',
    'border-accent-300/70 bg-accent-50/50 dark:border-accent-500/30 dark:bg-accent-950/10' => $note->pinned,
    'border-stone-200 dark:border-stone-800' => ! $note->pinned,
]) style="--i: {{ $index }}">
    <input type="checkbox" id="edit-note-{{ $note->id }}" class="peer sr-only" tabindex="-1" aria-hidden="true">

    <div class="flex items-start justify-between gap-3">
        <h2 class="min-w-0 break-words font-semibold leading-snug">{{ $note->title }}</h2>
        <form method="POST" action="{{ route('notes.pin', $note) }}">
            @csrf @method('PATCH')
            <button type="submit" title="{{ $note->pinned ? 'Unpin note' : 'Pin note' }}" aria-label="{{ $note->pinned ? 'Unpin note' : 'Pin note' }}"
                    @class([
                        'rounded-full p-2 transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-500/50 active:scale-90',
                        'text-accent-600 hover:bg-accent-500/15 dark:text-accent-400' => $note->pinned,
                        'text-stone-300 hover:bg-stone-100 hover:text-stone-500 dark:text-stone-600 dark:hover:bg-stone-800 dark:hover:text-stone-300' => ! $note->pinned,
                    ])>
                @if ($note->pinned)
                    <x-phosphor-push-pin-fill class="size-4 motion-safe:animate-pin-pop" />
                @else
                    <x-phosphor-push-pin class="size-4" />
                @endif
            </button>
        </form>
    </div>

    @if ($note->body)
        <p class="mt-1.5 whitespace-pre-line break-words text-sm leading-relaxed text-stone-600 line-clamp-[10] dark:text-stone-300">{{ $note->body }}</p>
    @endif

    <div class="mt-3 flex items-center justify-between">
        <time class="text-xs text-stone-400 dark:text-stone-500">{{ $note->updated_at->diffForHumans() }}</time>
        <div class="flex items-center gap-0.5">
            <label for="edit-note-{{ $note->id }}" role="button" tabindex="0" title="Edit note" aria-label="Edit note"
                   class="cursor-pointer rounded-full p-2 text-stone-400 transition hover:bg-stone-100 hover:text-stone-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-500/50 dark:text-stone-500 dark:hover:bg-stone-800 dark:hover:text-stone-300">
                <x-phosphor-pencil-simple class="size-4 peer-checked:hidden" />
                <x-phosphor-x class="size-4 hidden peer-checked:block" />
            </label>
            <form method="POST" action="{{ route('notes.destroy', $note) }}">
                @csrf @method('DELETE')
                <button type="submit" data-confirm title="Delete note" aria-label="Delete note"
                        class="rounded-full p-2 text-stone-400 transition hover:bg-stone-100 hover:text-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500/50 dark:text-stone-500 dark:hover:bg-stone-800 dark:hover:text-red-400">
                    <x-phosphor-trash class="size-4" />
                </button>
            </form>
        </div>
    </div>

    <form method="POST" action="{{ route('notes.update', $note) }}"
          class="mt-3 hidden flex-col gap-2 border-t border-stone-100 pt-3 peer-checked:flex peer-checked:motion-safe:animate-fade-up dark:border-stone-800">
        @csrf @method('PUT')
        <label class="sr-only" for="edit-title-{{ $note->id }}">Title</label>
        <input id="edit-title-{{ $note->id }}" name="title" required maxlength="255" value="{{ $note->title }}"
               class="rounded-lg border border-stone-300 bg-transparent px-3 py-2 text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
        <label class="sr-only" for="edit-body-{{ $note->id }}">Note</label>
        <textarea id="edit-body-{{ $note->id }}" name="body" rows="4"
                  class="resize-y rounded-lg border border-stone-300 bg-transparent px-3 py-2 text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">{{ $note->body }}</textarea>
        <div class="flex justify-end gap-2">
            <label for="edit-note-{{ $note->id }}" role="button" tabindex="0"
                   class="cursor-pointer rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-500/50 dark:text-stone-400 dark:hover:bg-stone-800">Cancel</label>
            <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Save</button>
        </div>
    </form>
</article>
