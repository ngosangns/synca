<!DOCTYPE html>
<html lang="{{ str_replace('_', '-', app()->getLocale()) }}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ config('app.name') }}</title>
    @vite(['resources/css/app.css', 'resources/js/app.js'])
</head>
<body class="min-h-dvh bg-stone-100 text-stone-900 antialiased dark:bg-stone-950 dark:text-stone-100">

<a href="#notes" class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-stone-900 focus:px-4 focus:py-2 focus:text-white dark:focus:bg-stone-100 dark:focus:text-stone-900">Skip to notes</a>

@if (session('status'))
    <div data-flash role="status"
         class="fixed left-1/2 top-4 z-50 flex -translate-x-1/2 items-center gap-2 rounded-full border border-stone-200 bg-white px-4 py-2 text-sm font-medium shadow-lg transition-all duration-300 dark:border-stone-700 dark:bg-stone-800">
        <svg class="size-4 text-emerald-600 dark:text-emerald-400" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M229.66,77.66l-128,128a8,8,0,0,1-11.32,0l-56-56a8,8,0,0,1,11.32-11.32L96,188.69,218.34,66.34a8,8,0,0,1,11.32,11.32Z"/></svg>
        {{ session('status') }}
    </div>
@endif

<header class="sticky top-0 z-40 border-b border-stone-200/80 bg-stone-100/80 backdrop-blur-md dark:border-stone-800 dark:bg-stone-950/80">
    <div class="mx-auto flex max-w-5xl items-center gap-4 px-5 py-3">
        <div class="flex items-center gap-2.5">
            <span class="flex size-8 items-center justify-center rounded-lg bg-amber-500/15 text-amber-600 dark:text-amber-400">
                <svg class="size-5" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M229.66,58.34l-32-32a8,8,0,0,0-11.32,0l-96,96A8,8,0,0,0,88,128v32a8,8,0,0,0,8,8h32a8,8,0,0,0,5.66-2.34l96-96A8,8,0,0,0,229.66,58.34ZM124.69,152H104V131.31l64-64L188.69,88ZM200,76.69,179.31,56,192,43.31,212.69,64ZM224,128v80a16,16,0,0,1-16,16H48a16,16,0,0,1-16-16V48A16,16,0,0,1,48,32h80a8,8,0,0,1,0,16H48V208H208V128a8,8,0,0,1,16,0Z"/></svg>
            </span>
            <h1 class="text-lg font-semibold tracking-tight">{{ config('app.name') }}</h1>
        </div>
        <form method="GET" action="{{ route('notes.index') }}" class="ml-auto w-full max-w-xs" role="search">
            <label class="sr-only" for="search">Search notes</label>
            <div class="relative">
                <svg class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-stone-400" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M229.66,218.34l-50.07-50.06a88.11,88.11,0,1,0-11.31,11.31l50.06,50.07a8,8,0,0,0,11.32-11.32ZM40,112a72,72,0,1,1,72,72A72.08,72.08,0,0,1,40,112Z"/></svg>
                <input id="search" data-search-input type="search" name="q" value="{{ $q }}"
                       placeholder="Search notes" autocomplete="off" spellcheck="false"
                       class="w-full rounded-full border border-stone-300 bg-white py-1.5 pl-9 pr-9 text-sm shadow-sm outline-none transition focus:border-amber-500 focus:ring-2 focus:ring-amber-500/30 dark:border-stone-700 dark:bg-stone-900 dark:placeholder-stone-500">
                @if ($q === '')
                    <kbd class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 rounded border border-stone-300 px-1.5 text-[10px] font-medium text-stone-400 dark:border-stone-600">/</kbd>
                @else
                    <a href="{{ route('notes.index') }}" aria-label="Clear search"
                       class="absolute right-2 top-1/2 -translate-y-1/2 rounded-full p-1 text-stone-400 transition hover:bg-stone-200 hover:text-stone-600 dark:hover:bg-stone-700 dark:hover:text-stone-200">
                        <svg class="size-3.5" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M205.66,194.34a8,8,0,0,1-11.32,11.32L128,139.31,61.66,205.66a8,8,0,0,1-11.32-11.32L116.69,128,50.34,61.66A8,8,0,0,1,61.66,50.34L128,116.69l66.34-66.35a8,8,0,0,1,11.32,11.32L128,139.31l66.34,66.35a8,8,0,0,1,11.32,0l11.32-11.32a8,8,0,0,0-11.32-11.32Z"/></svg>
                    </a>
                @endif
            </div>
        </form>
    </div>
</header>

<main class="mx-auto max-w-5xl px-5 py-6">

    <form method="POST" action="{{ route('notes.store') }}"
          class="mb-6 flex flex-col gap-2 rounded-xl border border-stone-200 bg-white p-4 shadow-sm focus-within:border-amber-500/60 focus-within:ring-2 focus-within:ring-amber-500/20 dark:border-stone-700 dark:bg-stone-900">
        @csrf
        <label class="sr-only" for="new-title">Title</label>
        <input id="new-title" name="title" required maxlength="255" placeholder="Take a note…" value="{{ old('title') }}" autofocus
               class="rounded-lg bg-transparent px-2 py-1.5 font-medium outline-none placeholder:text-stone-400 dark:placeholder-stone-500">
        <label class="sr-only" for="new-body">Note</label>
        <textarea id="new-body" name="body" rows="2" placeholder="Write something…"
                  class="resize-y rounded-lg bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-stone-400 dark:placeholder-stone-500">{{ old('body') }}</textarea>
        @error('title')<p class="px-2 text-sm text-red-600 dark:text-red-400">{{ $message }}</p>@enderror
        <div class="flex justify-end">
            <button class="rounded-full bg-stone-900 px-5 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Add note</button>
        </div>
    </form>

    @if ($q !== '')
        <p class="mb-4 text-sm text-stone-500 dark:text-stone-400">
            {{ $notes->total() }} {{ Str::plural('result', $notes->total()) }} for <span class="font-medium text-stone-700 dark:text-stone-200">&ldquo;{{ $q }}&rdquo;</span>
        </p>
    @endif

    <div id="notes" class="columns-1 gap-4 sm:columns-2 xl:columns-3">
        @forelse ($notes as $note)
            <article @class([
                'note-card mb-4 break-inside-avoid rounded-xl border bg-white p-4 shadow-sm dark:bg-stone-900',
                'border-amber-300/70 bg-amber-50/50 dark:border-amber-500/30 dark:bg-amber-950/10' => $note->pinned,
                'border-stone-200 dark:border-stone-800' => ! $note->pinned,
            ])>
                <details class="group">
                    <div class="flex items-start justify-between gap-3">
                        <h2 class="min-w-0 break-words font-semibold leading-snug">{{ $note->title }}</h2>
                        <form method="POST" action="{{ route('notes.pin', $note) }}">
                            @csrf @method('PATCH')
                            <button title="{{ $note->pinned ? 'Unpin note' : 'Pin note' }}" aria-label="{{ $note->pinned ? 'Unpin note' : 'Pin note' }}"
                                    @class([
                                        'rounded-full p-2 transition active:scale-90',
                                        'text-amber-600 hover:bg-amber-500/15 dark:text-amber-400' => $note->pinned,
                                        'text-stone-300 hover:bg-stone-100 hover:text-stone-500 dark:text-stone-600 dark:hover:bg-stone-800 dark:hover:text-stone-300' => ! $note->pinned,
                                    ])>
                                <svg class="size-4" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
                                    @if ($note->pinned)
                                        <path d="M235.33,104l-53.47,53.65c4.56,12.67,6.45,33.89-13.19,60A15.93,15.93,0,0,1,157,224c-.38,0-.75,0-1.13,0a16,16,0,0,1-11.32-4.69L96.29,171,53.66,213.66a8,8,0,0,1-11.32-11.32L85,159.71l-48.3-48.3A16,16,0,0,1,38,87.63c25.42-20.51,49.75-16.48,60.4-13.14L152,20.7a16,16,0,0,1,22.63,0l60.69,60.68A16,16,0,0,1,235.33,104Z"/>
                                    @else
                                        <path d="M235.32,81.37,174.63,20.69a16,16,0,0,0-22.63,0L98.37,74.49c-10.66-3.34-35-7.37-60.4,13.14a16,16,0,0,0-1.29,23.78L85,159.71,42.34,202.34a8,8,0,0,0,11.32,11.32L96.29,171l48.29,48.29A16,16,0,0,0,155.9,224c.38,0,.75,0,1.13,0a15.93,15.93,0,0,0,11.64-6.33c19.64-26.1,17.75-47.32,13.19-60L235.33,104A16,16,0,0,0,235.32,81.37ZM224,92.69h0l-57.27,57.46a8,8,0,0,0-1.49,9.22c9.46,18.93-1.8,38.59-9.34,48.62L48,100.08c12.08-9.74,23.64-12.31,32.48-12.31A40.13,40.13,0,0,1,96.81,91a8,8,0,0,0,9.25-1.51L163.32,32,224,92.68Z"/>
                                    @endif
                                </svg>
                            </button>
                        </form>
                    </div>

                    @if ($note->body)
                        <p class="mt-1.5 whitespace-pre-line break-words text-sm leading-relaxed text-stone-600 line-clamp-[10] dark:text-stone-300">{{ $note->body }}</p>
                    @endif

                    <div class="mt-3 flex items-center justify-between">
                        <time class="text-xs text-stone-400 dark:text-stone-500">{{ $note->updated_at->diffForHumans() }}</time>
                        <div class="flex items-center gap-0.5">
                            <summary title="Edit note" aria-label="Edit note"
                                     class="cursor-pointer list-none rounded-full p-2 text-stone-400 transition hover:bg-stone-100 hover:text-stone-600 dark:text-stone-500 dark:hover:bg-stone-800 dark:hover:text-stone-300 [&::-webkit-details-marker]:hidden">
                                <svg class="size-4" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M227.31,73.37,182.63,28.68a16,16,0,0,0-22.63,0L36.69,152A15.86,15.86,0,0,0,32,163.31V208a16,16,0,0,0,16,16H92.69A15.86,15.86,0,0,0,104,219.31L227.31,96a16,16,0,0,0,0-22.63ZM92.69,208H48V163.31l88-88L180.69,120ZM192,108.68,147.31,64l24-24L216,84.68Z"/></svg>
                            </summary>
                            <form method="POST" action="{{ route('notes.destroy', $note) }}">
                                @csrf @method('DELETE')
                                <button data-confirm title="Delete note" aria-label="Delete note"
                                        class="rounded-full p-2 text-stone-400 transition hover:bg-stone-100 hover:text-red-600 dark:text-stone-500 dark:hover:bg-stone-800 dark:hover:text-red-400">
                                    <svg class="size-4" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M216,48H176V40a24,24,0,0,0-24-24H104A24,24,0,0,0,80,40v8H40a8,8,0,0,0,0,16h8V208a16,16,0,0,0,16,16H192a16,16,0,0,0,16-16V64h8a8,8,0,0,0,0-16ZM96,40a8,8,0,0,1,8-8h48a8,8,0,0,1,8,8v8H96Zm96,168H64V64H192ZM112,104v64a8,8,0,0,1-16,0V104a8,8,0,0,1,16,0Zm48,0v64a8,8,0,0,1-16,0V104a8,8,0,0,1,16,0Z"/></svg>
                                </button>
                            </form>
                        </div>
                    </div>

                    <form method="POST" action="{{ route('notes.update', $note) }}"
                          class="mt-3 hidden flex-col gap-2 border-t border-stone-100 pt-3 group-open:flex dark:border-stone-800">
                        @csrf @method('PUT')
                        <label class="sr-only" for="edit-title-{{ $note->id }}">Title</label>
                        <input id="edit-title-{{ $note->id }}" name="title" required maxlength="255" value="{{ $note->title }}"
                               class="rounded-lg border border-stone-300 bg-transparent px-3 py-2 text-sm outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-500/30 dark:border-stone-700">
                        <label class="sr-only" for="edit-body-{{ $note->id }}">Note</label>
                        <textarea id="edit-body-{{ $note->id }}" name="body" rows="4"
                                  class="resize-y rounded-lg border border-stone-300 bg-transparent px-3 py-2 text-sm outline-none focus:border-amber-500 focus:ring-2 focus:ring-amber-500/30 dark:border-stone-700">{{ $note->body }}</textarea>
                        <div class="flex justify-end gap-2">
                            <button type="button" onclick="this.closest('details').removeAttribute('open')"
                                    class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 dark:text-stone-400 dark:hover:bg-stone-800">Cancel</button>
                            <button class="rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Save</button>
                        </div>
                    </form>
                </details>
            </article>
        @empty
            <div class="col-span-full flex flex-col items-center gap-3 py-20 text-center">
                <span class="flex size-14 items-center justify-center rounded-2xl bg-stone-200/70 text-stone-400 dark:bg-stone-800 dark:text-stone-500">
                    <svg class="size-7" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M229.66,58.34l-32-32a8,8,0,0,0-11.32,0l-96,96A8,8,0,0,0,88,128v32a8,8,0,0,0,8,8h32a8,8,0,0,0,5.66-2.34l96-96A8,8,0,0,0,229.66,58.34ZM124.69,152H104V131.31l64-64L188.69,88ZM200,76.69,179.31,56,192,43.31,212.69,64ZM224,128v80a16,16,0,0,1-16,16H48a16,16,0,0,1-16-16V48A16,16,0,0,1,48,32h80a8,8,0,0,1,0,16H48V208H208V128a8,8,0,0,1,16,0Z"/></svg>
                </span>
                @if ($q !== '')
                    <p class="text-sm text-stone-500 dark:text-stone-400">No notes match <span class="font-medium text-stone-700 dark:text-stone-200">&ldquo;{{ $q }}&rdquo;</span>.</p>
                    <a href="{{ route('notes.index') }}" class="text-sm font-medium text-amber-600 transition hover:text-amber-500 dark:text-amber-400">Clear search</a>
                @else
                    <p class="text-sm text-stone-500 dark:text-stone-400">No notes yet. Write your first one above.</p>
                @endif
            </div>
        @endforelse
    </div>

    @if ($notes->hasPages())
        <nav class="mt-6" aria-label="Notes pages">{{ $notes->links() }}</nav>
    @endif
</main>
</body>
</html>
