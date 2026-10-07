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
         class="fixed left-1/2 top-10 z-50 flex -translate-x-1/2 items-center gap-2 rounded-full border border-stone-200 bg-white px-4 py-2 text-sm font-medium shadow-lg transition-all duration-300 motion-safe:animate-toast-in dark:border-stone-700 dark:bg-stone-800">
        <x-phosphor-check-circle-fill class="size-4 text-accent-600 dark:text-accent-400" />
        {{ session('status') }}
    </div>
@endif

<header class="drag-region sticky top-0 z-40 border-b border-stone-200/80 bg-stone-100/80 backdrop-blur-xl dark:border-stone-800 dark:bg-stone-950/80">
    <div class="h-7 shrink-0" aria-hidden="true"></div>
    <div id="page-progress" aria-hidden="true"></div>
    <div class="mx-auto flex max-w-5xl items-center gap-4 px-5 pb-3">
        <div class="flex select-none items-center gap-2.5">
            <span class="flex size-8 items-center justify-center rounded-lg bg-accent-500/15 text-accent-600 dark:text-accent-400">
                <x-phosphor-note-pencil class="size-5" />
            </span>
            <h1 class="text-lg font-semibold tracking-tight">{{ config('app.name') }}</h1>
        </div>
        <form method="GET" action="{{ route('notes.index') }}" data-search-form class="no-drag ml-auto w-full max-w-xs" role="search">
            <label class="sr-only" for="search">Search notes</label>
            <div class="relative">
                <x-phosphor-magnifying-glass class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-stone-400" />
                <input id="search" data-search-input type="search" name="q" value="{{ $q }}"
                       placeholder="Search notes" autocomplete="off" spellcheck="false"
                       class="w-full rounded-full border border-stone-300 bg-white py-1.5 pl-9 pr-9 text-sm shadow-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700 dark:bg-stone-900 dark:placeholder-stone-500">
                @if ($q === '')
                    <kbd class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 rounded border border-stone-300 px-1.5 text-[10px] font-medium text-stone-400 dark:border-stone-600">/</kbd>
                @else
                    <a href="{{ route('notes.index') }}" data-clear-search aria-label="Clear search"
                       class="absolute right-2 top-1/2 -translate-y-1/2 rounded-full p-1 text-stone-400 transition hover:bg-stone-200 hover:text-stone-600 dark:hover:bg-stone-700 dark:hover:text-stone-200">
                        <x-phosphor-x class="size-3.5" />
                    </a>
                @endif
            </div>
        </form>
    </div>
</header>

<main class="mx-auto max-w-5xl px-5 py-6">

    <form method="POST" action="{{ route('notes.store') }}"
          class="mb-6 flex flex-col gap-2 rounded-xl border border-stone-200 bg-white p-4 shadow-sm transition focus-within:border-accent-500/60 focus-within:ring-2 focus-within:ring-accent-500/20 dark:border-stone-700 dark:bg-stone-900">
        @csrf
        <label class="sr-only" for="new-title">Title</label>
        <input id="new-title" name="title" required maxlength="255" placeholder="Take a note…" value="{{ old('title') }}" autofocus
               class="rounded-lg bg-transparent px-2 py-1.5 font-medium outline-none placeholder:text-stone-400 dark:placeholder-stone-500">
        <label class="sr-only" for="new-body">Note</label>
        <textarea id="new-body" name="body" rows="2" placeholder="Write something…"
                  class="resize-y rounded-lg bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-stone-400 dark:placeholder-stone-500">{{ old('body') }}</textarea>
        @error('title')<p class="px-2 text-sm text-red-600 dark:text-red-400">{{ $message }}</p>@enderror
        <div class="flex justify-end">
            <button type="submit" class="btn-primary inline-flex items-center gap-1.5 rounded-full bg-stone-900 px-5 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">
                <x-phosphor-plus-bold class="size-3.5" />
                Add note
            </button>
        </div>
    </form>

    <div id="notes-region">
        @include('notes._region')
    </div>
</main>

<template id="note-skeleton">
    <div class="mb-4 break-inside-avoid rounded-xl border border-stone-200 bg-white p-4 dark:border-stone-800 dark:bg-stone-900" aria-hidden="true">
        <div class="skeleton-bar h-4 w-2/3 rounded"></div>
        <div class="skeleton-bar mt-3 h-3 w-full rounded"></div>
        <div class="skeleton-bar mt-2 h-3 w-5/6 rounded"></div>
        <div class="skeleton-bar mt-2 h-3 w-1/3 rounded"></div>
    </div>
</template>
</body>
</html>
