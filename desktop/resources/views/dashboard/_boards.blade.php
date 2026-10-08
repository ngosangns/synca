{{-- Col 1: Skills + MCPs lists --}}
<div class="grid min-h-0 grid-rows-[55fr_45fr] gap-4">

    <section class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900">
        <div class="flex shrink-0 items-center gap-1.5 border-b border-stone-100 px-3 py-2 dark:border-stone-800">
            <h2 class="flex items-center gap-2 text-sm font-semibold">
                <x-phosphor-package class="size-4 text-accent-600 dark:text-accent-400" />
                Skills
                <span class="text-xs font-normal text-stone-400">{{ count($skills) }}</span>
            </h2>
            <div class="ml-auto flex items-center gap-0.5">
                <form method="POST" action="{{ route('sync.plan') }}">
                    @csrf
                    <input type="hidden" name="scope" value="{{ $scope }}">
                    <input type="hidden" name="target" value="skills">
                    <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                    <button type="submit" title="Sync all skills" aria-label="Sync all skills"
                            class="rounded-full p-1.5 text-stone-400 transition hover:bg-accent-500/15 hover:text-accent-600 dark:hover:text-accent-400">
                        <x-phosphor-arrows-left-right class="size-3.5" />
                    </button>
                </form>
                <a href="{{ route('dashboard', ['scope' => $scope, 'section' => 'skills', 'panel' => 'install-skill']) }}" data-nav
                   title="Install skill" aria-label="Install skill"
                   class="rounded-full p-1.5 text-stone-400 transition hover:bg-accent-500/15 hover:text-accent-600 dark:hover:text-accent-400">
                    <x-phosphor-plus class="size-3.5" />
                </a>
            </div>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
            @forelse ($skills as $s)
                <a href="{{ route('dashboard', ['scope' => $scope, 'section' => 'skills', 'key' => $s['key']]) }}" data-nav
                   @class([
                       'group flex items-center gap-2 rounded-lg px-2.5 py-2 text-sm transition',
                       'bg-accent-500/10 text-accent-800 dark:bg-accent-500/15 dark:text-accent-200' => $section === 'skills' && $key === $s['key'],
                       'hover:bg-stone-100 dark:hover:bg-stone-800/70' => ! ($section === 'skills' && $key === $s['key']),
                   ])>
                    @if ($s['mismatch'])
                        <x-phosphor-warning-fill class="size-3.5 shrink-0 text-red-500" />
                    @else
                        <x-phosphor-package class="size-3.5 shrink-0 text-stone-300 dark:text-stone-600" />
                    @endif
                    <span class="min-w-0 truncate font-medium">{{ $s['display_name'] ?? $s['key'] }}</span>
                    <span class="ml-auto shrink-0 text-[11px] text-stone-400">{{ count($s['presence'] ?? []) }}</span>
                </a>
            @empty
                <p class="px-3 py-6 text-center text-xs text-stone-400">No skills in this scope.</p>
            @endforelse
        </div>
    </section>

    <section class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-900">
        <div class="flex shrink-0 items-center gap-1.5 border-b border-stone-100 px-3 py-2 dark:border-stone-800">
            <h2 class="flex items-center gap-2 text-sm font-semibold">
                <x-phosphor-plugs-connected class="size-4 text-accent-600 dark:text-accent-400" />
                MCPs
                <span class="text-xs font-normal text-stone-400">{{ count($mcps) }}</span>
            </h2>
            <div class="ml-auto flex items-center gap-0.5">
                <form method="POST" action="{{ route('sync.plan') }}">
                    @csrf
                    <input type="hidden" name="scope" value="{{ $scope }}">
                    <input type="hidden" name="target" value="mcp">
                    <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                    <button type="submit" title="Sync all MCPs" aria-label="Sync all MCPs"
                            class="rounded-full p-1.5 text-stone-400 transition hover:bg-accent-500/15 hover:text-accent-600 dark:hover:text-accent-400">
                        <x-phosphor-arrows-left-right class="size-3.5" />
                    </button>
                </form>
                <a href="{{ route('dashboard', ['scope' => $scope, 'section' => 'mcps', 'panel' => 'add-mcp']) }}" data-nav
                   title="Add MCP" aria-label="Add MCP"
                   class="rounded-full p-1.5 text-stone-400 transition hover:bg-accent-500/15 hover:text-accent-600 dark:hover:text-accent-400">
                    <x-phosphor-plus class="size-3.5" />
                </a>
            </div>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
            @forelse ($mcps as $m)
                <a href="{{ route('dashboard', ['scope' => $scope, 'section' => 'mcps', 'key' => $m['key']]) }}" data-nav
                   @class([
                       'group flex items-center gap-2 rounded-lg px-2.5 py-2 text-sm transition',
                       'bg-accent-500/10 text-accent-800 dark:bg-accent-500/15 dark:text-accent-200' => $section === 'mcps' && $key === $m['key'],
                       'hover:bg-stone-100 dark:hover:bg-stone-800/70' => ! ($section === 'mcps' && $key === $m['key']),
                   ])>
                    @if ($m['mismatch'])
                        <x-phosphor-warning-fill class="size-3.5 shrink-0 text-red-500" />
                    @else
                        <x-phosphor-plugs class="size-3.5 shrink-0 text-stone-300 dark:text-stone-600" />
                    @endif
                    <span class="min-w-0 truncate font-medium">{{ $m['key'] }}</span>
                    <span class="ml-auto shrink-0 text-[11px] text-stone-400">{{ count($m['presence'] ?? []) }}</span>
                </a>
            @empty
                <p class="px-3 py-6 text-center text-xs text-stone-400">No MCP servers in this scope.</p>
            @endforelse
        </div>
    </section>
</div>

{{-- Col 2: Detail / panel / plan --}}
<section id="detail-pane" class="min-h-0 overflow-y-auto rounded-xl border border-stone-200 bg-white p-5 dark:border-stone-800 dark:bg-stone-900">
    @include('dashboard._detail')
</section>
