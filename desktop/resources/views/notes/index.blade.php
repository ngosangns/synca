<!DOCTYPE html>
<html lang="{{ str_replace('_', '-', app()->getLocale()) }}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ config('app.name') }}</title>
    @vite(['resources/css/app.css', 'resources/js/app.js'])
</head>
<body class="min-h-screen bg-neutral-50 text-neutral-900 antialiased dark:bg-neutral-900 dark:text-neutral-100">
<div class="mx-auto flex max-w-3xl flex-col gap-6 p-6">
    <header class="flex items-center justify-between gap-4">
        <h1 class="text-2xl font-semibold">{{ config('app.name') }}</h1>
        <form method="GET" action="{{ route('notes.index') }}">
            <input type="search" name="q" value="{{ $q }}" placeholder="Search notes"
                   class="w-56 rounded-md border border-neutral-300 bg-white px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800">
        </form>
    </header>

    <form method="POST" action="{{ route('notes.store') }}" id="new-note"
          class="flex flex-col gap-2 rounded-lg border border-neutral-200 bg-white p-4 shadow-sm dark:border-neutral-700 dark:bg-neutral-800">
        @csrf
        <input name="title" required maxlength="255" placeholder="Title" value="{{ old('title') }}" autofocus
               class="rounded-md border border-neutral-300 bg-transparent px-3 py-2 dark:border-neutral-600">
        <textarea name="body" rows="3" placeholder="Write something…"
                  class="rounded-md border border-neutral-300 bg-transparent px-3 py-2 dark:border-neutral-600">{{ old('body') }}</textarea>
        @error('title')<p class="text-sm text-red-600">{{ $message }}</p>@enderror
        <div class="flex justify-end">
            <button class="rounded-md bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900">Add note</button>
        </div>
    </form>

    <ul class="flex flex-col gap-3">
        @forelse ($notes as $note)
            <li class="rounded-lg border border-neutral-200 bg-white p-4 shadow-sm dark:border-neutral-700 dark:bg-neutral-800">
                <details>
                    <summary class="flex cursor-pointer items-start justify-between gap-4">
                        <div>
                            <p class="font-medium">@if($note->pinned)<span title="Pinned">📌 </span>@endif{{ $note->title }}</p>
                            @if ($note->body)<p class="mt-1 whitespace-pre-line text-sm text-neutral-600 dark:text-neutral-300">{{ $note->body }}</p>@endif
                            <p class="mt-2 text-xs text-neutral-400">{{ $note->updated_at->diffForHumans() }}</p>
                        </div>
                        <div class="flex shrink-0 gap-2 text-sm">
                            <form method="POST" action="{{ route('notes.pin', $note) }}">@csrf @method('PATCH')
                                <button class="text-neutral-500 hover:text-neutral-900 dark:hover:text-white">{{ $note->pinned ? 'Unpin' : 'Pin' }}</button>
                            </form>
                            <form method="POST" action="{{ route('notes.destroy', $note) }}">@csrf @method('DELETE')
                                <button class="text-red-500 hover:text-red-700">Delete</button>
                            </form>
                        </div>
                    </summary>
                    <form method="POST" action="{{ route('notes.update', $note) }}" class="mt-3 flex flex-col gap-2">
                        @csrf @method('PUT')
                        <input name="title" required maxlength="255" value="{{ $note->title }}"
                               class="rounded-md border border-neutral-300 bg-transparent px-3 py-2 dark:border-neutral-600">
                        <textarea name="body" rows="4" class="rounded-md border border-neutral-300 bg-transparent px-3 py-2 dark:border-neutral-600">{{ $note->body }}</textarea>
                        <div class="flex justify-end"><button class="rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-600">Save</button></div>
                    </form>
                </details>
            </li>
        @empty
            <li class="py-12 text-center text-neutral-400">No notes yet.</li>
        @endforelse
    </ul>
</div>
</body>
</html>
