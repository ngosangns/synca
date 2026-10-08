<!DOCTYPE html>
<html lang="{{ str_replace('_', '-', app()->getLocale()) }}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ config('app.name') }}</title>
    @vite(['resources/css/app.css', 'resources/js/app.js'])
</head>
<body class="flex h-dvh flex-col overflow-hidden bg-stone-100 text-stone-900 antialiased dark:bg-stone-950 dark:text-stone-100">

<a href="#detail-pane" class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-stone-900 focus:px-4 focus:py-2 focus:text-white dark:focus:bg-stone-100 dark:focus:text-stone-900">Skip to detail</a>

@if (session('status'))
    <div data-flash role="status"
         class="fixed left-1/2 top-10 z-50 flex -translate-x-1/2 items-center gap-2 rounded-full border border-stone-200 bg-white px-4 py-2 text-sm font-medium shadow-lg transition-all duration-300 motion-safe:animate-toast-in dark:border-stone-700 dark:bg-stone-800">
        <x-phosphor-check-circle-fill class="size-4 text-accent-600 dark:text-accent-400" />
        {{ session('status') }}
    </div>
@endif

<header class="drag-region shrink-0 border-b border-stone-200/80 bg-stone-100/80 backdrop-blur-xl dark:border-stone-800 dark:bg-stone-950/80">
    <div class="h-7 shrink-0" aria-hidden="true"></div>
    <div id="page-progress" aria-hidden="true"></div>
    <div class="mx-auto flex max-w-[1400px] items-center gap-4 px-5 pb-3">
        <div class="flex select-none items-center gap-2.5">
            <span class="flex size-8 items-center justify-center rounded-lg bg-accent-500/15 text-accent-600 dark:text-accent-400">
                <x-phosphor-arrows-clockwise class="size-5" />
            </span>
            <h1 class="text-lg font-semibold tracking-tight">synca</h1>
        </div>

        {{-- Scope tabs (User / Project), like the TUI Tab key. Full reload so
             the tab state + project-dir field stay in sync. --}}
        <nav class="no-drag flex items-center rounded-full border border-stone-300 bg-white p-0.5 text-sm dark:border-stone-700 dark:bg-stone-900" aria-label="Scope">
            @foreach (['user' => 'User', 'project' => 'Project'] as $s => $label)
                <a href="{{ route('dashboard', ['scope' => $s]) }}" data-scope-tab="{{ $s }}"
                   @class([
                       'rounded-full px-3.5 py-1 font-medium transition',
                       'bg-stone-900 text-white dark:bg-stone-100 dark:text-stone-900' => $scope === $s,
                       'text-stone-500 hover:text-stone-800 dark:text-stone-400 dark:hover:text-stone-200' => $scope !== $s,
                   ])>{{ $label }}</a>
            @endforeach
        </nav>

        @if ($scope === 'project')
            <form method="POST" action="{{ route('settings.project-dir') }}" class="no-drag flex min-w-0 flex-1 items-center gap-2">
                @csrf
                <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                <label class="sr-only" for="project-dir">Project directory</label>
                <input id="project-dir" name="dir" value="{{ $projectDir }}" spellcheck="false"
                       class="w-full min-w-0 rounded-full border border-stone-300 bg-white px-3 py-1 font-mono text-xs text-stone-600 outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700 dark:bg-stone-900 dark:text-stone-300">
            </form>
        @endif

        <div class="no-drag ml-auto flex items-center gap-1.5">
            <form method="POST" action="{{ route('sync.plan') }}" data-syncall-form>
                @csrf
                <input type="hidden" name="scope" value="{{ $scope }}">
                <input type="hidden" name="target" value="all">
                <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                <button type="submit" title="Sync all skills + MCPs" aria-label="Sync all skills and MCPs"
                        class="inline-flex items-center gap-1.5 rounded-full bg-stone-900 px-3.5 py-1.5 text-xs font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">
                    <x-phosphor-arrows-merge class="size-3.5" /> Sync all
                </button>
            </form>
            <span class="mx-0.5 h-4 w-px bg-stone-300 dark:bg-stone-700" aria-hidden="true"></span>
            <a href="{{ request()->fullUrl() }}" data-refresh title="Reload inventory" aria-label="Reload inventory"
               class="rounded-full p-2 text-stone-400 transition hover:bg-stone-200 hover:text-stone-600 dark:hover:bg-stone-800 dark:hover:text-stone-300">
                <x-phosphor-arrow-clockwise class="size-4" />
            </a>
            <form method="POST" action="{{ route('update.check') }}" data-update-form>
                @csrf
                <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope, 'panel' => 'update']) }}">
                <button type="submit" title="Check for synca updates" aria-label="Check for synca updates"
                        class="rounded-full p-2 text-stone-400 transition hover:bg-stone-200 hover:text-stone-600 dark:hover:bg-stone-800 dark:hover:text-stone-300">
                    <x-phosphor-cloud-arrow-down class="size-4" />
                </button>
            </form>
            <details id="help-pop" class="group relative">
                <summary title="Help" aria-label="Help and shortcuts"
                         class="cursor-pointer list-none rounded-full p-2 text-stone-400 transition hover:bg-stone-200 hover:text-stone-600 group-open:bg-stone-200 group-open:text-stone-700 dark:hover:bg-stone-800 dark:hover:text-stone-300 dark:group-open:bg-stone-800 [&::-webkit-details-marker]:hidden">
                    <x-phosphor-question class="size-4" />
                </summary>
                <div class="absolute right-0 top-full z-40 mt-2 w-72 rounded-xl border border-stone-200 bg-white p-4 text-sm shadow-xl motion-safe:animate-fade-up dark:border-stone-700 dark:bg-stone-900">
                    <p class="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-stone-400">
                        <x-phosphor-keyboard class="size-4" /> Shortcuts
                    </p>
                    <dl class="grid grid-cols-[64px_1fr] gap-y-1.5 text-xs">
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">j / k</kbd></dt><dd class="text-stone-500 dark:text-stone-400">move selection</dd>
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">1 / 2</kbd></dt><dd class="text-stone-500 dark:text-stone-400">user / project scope</dd>
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">r</kbd></dt><dd class="text-stone-500 dark:text-stone-400">reload inventory</dd>
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">u</kbd></dt><dd class="text-stone-500 dark:text-stone-400">check for updates</dd>
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">s</kbd></dt><dd class="text-stone-500 dark:text-stone-400">sync everything</dd>
                        <dt><kbd class="rounded bg-stone-100 px-1.5 py-0.5 font-mono dark:bg-stone-800">?</kbd></dt><dd class="text-stone-500 dark:text-stone-400">this help</dd>
                    </dl>
                    <p class="mt-3 border-t border-stone-100 pt-2 text-[11px] leading-relaxed text-stone-400 dark:border-stone-800">
                        Every action runs the <code class="font-mono">synca</code> CLI and is written to the log column. Sync always shows a dry-run plan first.
                    </p>
                </div>
            </details>
        </div>
    </div>
</header>

<main class="mx-auto grid w-full max-w-[1400px] min-h-0 flex-1 grid-cols-[280px_minmax(0,1fr)_340px] gap-4 px-5 py-4">

    <div id="boards-region" class="contents">
        @include('dashboard._boards')
    </div>

    {{-- Log pane: dedicated right column --}}
    <aside id="logs-column" class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900">
        <div class="flex shrink-0 items-center justify-between border-b border-stone-100 px-4 py-2.5 dark:border-stone-800">
            <h2 class="flex items-center gap-2 text-sm font-semibold">
                <x-phosphor-terminal class="size-4 text-stone-400" />
                Log
            </h2>
            <form method="POST" action="{{ route('logs.clear') }}">
                @csrf
                <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                <button type="submit" title="Clear log" aria-label="Clear log"
                        class="rounded-full p-1.5 text-stone-400 transition hover:bg-stone-100 hover:text-stone-600 dark:hover:bg-stone-800 dark:hover:text-stone-300">
                    <x-phosphor-broom class="size-3.5" />
                </button>
            </form>
        </div>
        <div id="logs-scroll" class="min-h-0 flex-1 overflow-y-auto px-3 py-2">
            <div id="logs-list">
                @include('dashboard._logs')
            </div>
            <div id="logs-meta" data-oldest="{{ $logs->last()?->id ?? '' }}" data-newest="{{ $logs->first()?->id ?? '' }}" hidden></div>
            <div id="logs-sentinel" class="h-px" aria-hidden="true"></div>
        </div>
    </aside>
</main>

<template id="log-skeleton">
    <div class="px-1 py-2" aria-hidden="true">
        <div class="skeleton-bar h-3 w-3/4 rounded"></div>
        <div class="skeleton-bar mt-1.5 h-2.5 w-1/2 rounded"></div>
    </div>
</template>
</body>
</html>
