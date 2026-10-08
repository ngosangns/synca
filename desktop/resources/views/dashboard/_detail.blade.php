@php
    $shortHash = fn (?string $h) => $h ? substr($h, 0, 8) : '-';
@endphp

@if (! $available)
    <div class="flex h-full flex-col items-center justify-center gap-3 text-center">
        <span class="flex size-14 items-center justify-center rounded-2xl bg-red-500/10 text-red-500">
            <x-phosphor-warning class="size-7" />
        </span>
        <p class="text-sm font-medium">synca binary not found</p>
        <p class="max-w-sm text-sm text-stone-500 dark:text-stone-400">
            Expected at <code class="rounded bg-stone-100 px-1.5 py-0.5 font-mono text-xs dark:bg-stone-800">{{ $bin }}</code>.
            Install synca or set <code class="rounded bg-stone-100 px-1.5 py-0.5 font-mono text-xs dark:bg-stone-800">SYNCA_BIN</code>.
        </p>
    </div>

@elseif ($plan)
    {{-- Dry-run plan preview (sync or install/add) --}}
    <div class="flex flex-col gap-4">
        <div class="flex items-center gap-2">
            <span @class([
                'flex size-8 items-center justify-center rounded-lg',
                'bg-accent-500/15 text-accent-600 dark:text-accent-400' => $plan['ok'],
                'bg-red-500/10 text-red-500' => ! $plan['ok'],
            ])>
                <x-phosphor-eye class="size-4" />
            </span>
            <div>
                <h2 class="font-semibold leading-tight">Plan · {{ $plan['title'] }}</h2>
                <p class="text-xs text-stone-400">scope {{ $plan['scope'] }} · dry-run</p>
            </div>
            @unless ($plan['ok'])
                <span class="rounded-full bg-red-500/10 px-2.5 py-0.5 text-xs font-medium text-red-600 dark:text-red-400">failed</span>
            @endunless
        </div>

        @if (! empty($plan['summary']))
            <div class="flex flex-wrap items-center gap-1.5">
                @foreach ($plan['summary'] as $kind => $n)
                    <span @class([
                        'rounded-full px-2.5 py-0.5 font-mono text-xs',
                        'bg-stone-100 text-stone-500 dark:bg-stone-800 dark:text-stone-400' => str_starts_with($kind, 'skip'),
                        'bg-accent-500/15 text-accent-700 dark:text-accent-300' => str_starts_with($kind, 'conflict'),
                        'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' => ! str_starts_with($kind, 'skip') && ! str_starts_with($kind, 'conflict'),
                    ])>{{ $kind }} × {{ $n }}</span>
                @endforeach
            </div>
        @endif

        <details class="rounded-lg border border-stone-200 dark:border-stone-700">
            <summary class="cursor-pointer select-none list-none px-3 py-2 text-xs font-medium text-stone-500 transition hover:text-stone-700 dark:hover:text-stone-300 [&::-webkit-details-marker]:hidden">
                Plan output
            </summary>
            <pre class="max-h-64 overflow-auto border-t border-stone-100 p-3 font-mono text-xs leading-relaxed text-stone-600 dark:border-stone-800 dark:text-stone-300">{{ $plan['output'] }}</pre>
        </details>

        @if ($plan['ok'] && ! empty($plan['install_source']))
            <form method="POST" action="{{ route('skills.install') }}" class="flex items-center justify-end gap-2">
                @csrf
                <input type="hidden" name="scope" value="{{ $plan['scope'] }}">
                <input type="hidden" name="source" value="{{ $plan['install_source'] }}">
                <input type="hidden" name="apply" value="1">
                <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
                <a href="{{ route('dashboard', ['scope' => $scope]) }}" data-nav
                   class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 dark:text-stone-400 dark:hover:bg-stone-800">Cancel</a>
                <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Apply install</button>
            </form>

        @elseif ($plan['ok'] && ! empty($plan['mcp_payload']))
            <form method="POST" action="{{ route('mcp.add') }}" class="flex items-center justify-end gap-2">
                @csrf
                <input type="hidden" name="scope" value="{{ $plan['scope'] }}">
                @foreach ($plan['mcp_payload'] as $f => $v)
                    @if ($v !== null && $v !== '')
                        <input type="hidden" name="{{ $f }}" value="{{ $v }}">
                    @endif
                @endforeach
                <input type="hidden" name="apply" value="1">
                <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
                <a href="{{ route('dashboard', ['scope' => $scope]) }}" data-nav
                   class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 dark:text-stone-400 dark:hover:bg-stone-800">Cancel</a>
                <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Apply add</button>
            </form>

        @elseif ($plan['ok'])
            <form method="POST" action="{{ route('sync.apply') }}" data-decisions-form class="flex flex-col gap-3">
                @csrf
                <input type="hidden" name="scope" value="{{ $plan['scope'] }}">
                <input type="hidden" name="target" value="{{ $plan['target'] }}">
                @if ($plan['key'])<input type="hidden" name="key" value="{{ $plan['key'] }}">@endif
                <input type="hidden" name="decisions" data-decisions-json value="{}">
                <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">

                @if (! empty($plan['conflicts']))
                    <div class="rounded-lg border border-accent-300/70 bg-accent-50/50 p-3 dark:border-accent-500/30 dark:bg-accent-950/10">
                        <p class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-accent-700 dark:text-accent-300">
                            <x-phosphor-warning class="size-3.5" />
                            {{ count($plan['conflicts']) }} conflict(s) — pick a policy per item
                        </p>
                        <div class="flex flex-col gap-1.5">
                            @foreach ($plan['conflicts'] as $c)
                                <div class="flex items-center gap-2 text-xs">
                                    <span @class([
                                        'rounded px-1.5 py-0.5 font-mono font-medium',
                                        'bg-accent-500/15 text-accent-700 dark:text-accent-300' => $c['kind'] === 'skill',
                                        'bg-violet-500/15 text-violet-700 dark:text-violet-300' => $c['kind'] === 'mcp',
                                    ])>{{ $c['kind'] }}</span>
                                    <span class="min-w-0 flex-1 truncate font-mono">{{ $c['key'] }}</span>
                                    <select data-conflict data-kind="{{ $c['kind'] === 'skill' ? 'skills' : 'mcps' }}" data-key="{{ $c['key'] }}"
                                            class="rounded-md border border-stone-300 bg-white px-2 py-1 text-xs outline-none focus:border-accent-500 dark:border-stone-600 dark:bg-stone-800">
                                        <option value="skip" selected>skip</option>
                                        <option value="keep-source">keep-source</option>
                                        <option value="keep-target">keep-target</option>
                                    </select>
                                </div>
                            @endforeach
                        </div>
                    </div>
                @endif

                <div class="flex items-center justify-end gap-2">
                    <a href="{{ request()->fullUrl() }}" data-nav
                       class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 dark:text-stone-400 dark:hover:bg-stone-800">Cancel</a>
                    <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Apply sync</button>
                </div>
            </form>
        @else
            <div class="flex justify-end">
                <a href="{{ request()->fullUrl() }}" data-nav
                   class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 dark:text-stone-400 dark:hover:bg-stone-800">Dismiss</a>
            </div>
        @endif
    </div>

@elseif ($panel === 'install-skill')
    <div class="flex flex-col gap-4">
        <h2 class="flex items-center gap-2 font-semibold">
            <x-phosphor-plus class="size-4 text-accent-600 dark:text-accent-400" />
            Install skill
        </h2>
        <p class="text-sm text-stone-500 dark:text-stone-400">Local path or git URL containing a <code class="rounded bg-stone-100 px-1 py-0.5 font-mono text-xs dark:bg-stone-800">SKILL.md</code>. A dry-run plan is shown before applying.</p>
        <form method="POST" action="{{ route('skills.install') }}" class="flex flex-col gap-3">
            @csrf
            <input type="hidden" name="scope" value="{{ $scope }}">
            <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
            <div>
                <label for="skill-source" class="mb-1 block text-xs font-medium text-stone-500 dark:text-stone-400">Source</label>
                <input id="skill-source" name="source" required placeholder="~/path/to/skill or https://github.com/user/repo" autofocus
                       class="w-full rounded-lg border border-stone-300 bg-transparent px-3 py-2 font-mono text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
            </div>
            <div class="flex justify-end">
                <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Preview plan</button>
            </div>
        </form>
    </div>

@elseif ($panel === 'add-mcp')
    <div class="flex flex-col gap-4">
        <h2 class="flex items-center gap-2 font-semibold">
            <x-phosphor-plus class="size-4 text-accent-600 dark:text-accent-400" />
            Add MCP server
        </h2>
        <form method="POST" action="{{ route('mcp.add') }}" class="flex flex-col gap-3">
            @csrf
            <input type="hidden" name="scope" value="{{ $scope }}">
            <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
            <div class="grid grid-cols-2 gap-3">
                <div>
                    <label for="mcp-name" class="mb-1 block text-xs font-medium text-stone-500 dark:text-stone-400">Name</label>
                    <input id="mcp-name" name="name" required pattern="[A-Za-z0-9._ -]+" placeholder="my-server" autofocus
                           class="w-full rounded-lg border border-stone-300 bg-transparent px-3 py-2 font-mono text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
                </div>
                <div>
                    <label for="mcp-transport" class="mb-1 block text-xs font-medium text-stone-500 dark:text-stone-400">Transport</label>
                    <select id="mcp-transport" name="transport" data-mcp-transport
                            class="w-full rounded-lg border border-stone-300 bg-transparent px-3 py-2 text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
                        <option value="stdio" selected>stdio</option>
                        <option value="http">http</option>
                        <option value="sse">sse</option>
                    </select>
                </div>
            </div>
            <div data-mcp-command>
                <label for="mcp-command" class="mb-1 block text-xs font-medium text-stone-500 dark:text-stone-400">Command</label>
                <input id="mcp-command" name="command" placeholder="npx -y @pkg/server"
                       class="w-full rounded-lg border border-stone-300 bg-transparent px-3 py-2 font-mono text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
            </div>
            <div data-mcp-url class="hidden">
                <label for="mcp-url" class="mb-1 block text-xs font-medium text-stone-500 dark:text-stone-400">URL</label>
                <input id="mcp-url" name="url" placeholder="http://127.0.0.1:9000/mcp"
                       class="w-full rounded-lg border border-stone-300 bg-transparent px-3 py-2 font-mono text-sm outline-none transition focus:border-accent-500 focus:ring-2 focus:ring-accent-500/30 dark:border-stone-700">
            </div>
            <label class="flex cursor-pointer items-center gap-2 text-sm text-stone-600 dark:text-stone-300">
                <input type="checkbox" name="enabled" value="1" checked class="size-4 rounded border-stone-300 text-accent-600 accent-amber-600">
                Enabled
            </label>
            <div class="flex justify-end">
                <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Preview plan</button>
            </div>
        </form>
    </div>

@elseif ($panel === 'update' || $update)
    <div class="flex flex-col gap-4">
        <h2 class="flex items-center gap-2 font-semibold">
            <x-phosphor-cloud-arrow-down class="size-4 text-accent-600 dark:text-accent-400" />
            Update synca
        </h2>
        @if ($update)
            <div class="rounded-lg border border-stone-200 p-4 text-sm dark:border-stone-700">
                <p class="font-medium">{{ $update['message'] ?? '' }}</p>
                @if (! empty($update['current']))
                    <p class="mt-1 font-mono text-xs text-stone-500 dark:text-stone-400">
                        current {{ $update['current'] }}@if (! empty($update['latest'])) → latest {{ $update['latest'] }}@endif
                    </p>
                @endif
            </div>
            @if ($update['update_available'] ?? false)
                <form method="POST" action="{{ route('update.install') }}" class="flex justify-end">
                    @csrf
                    <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                    <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Install update</button>
                </form>
            @endif
        @else
            <p class="text-sm text-stone-500 dark:text-stone-400">Check GitHub Releases for a newer synca binary.</p>
            <form method="POST" action="{{ route('update.check') }}" class="flex justify-end">
                @csrf
                <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                <button type="submit" class="btn-primary rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">Check now</button>
            </form>
        @endif
    </div>

@elseif ($selected)
    @if ($section === 'skills')
        <div class="flex flex-col gap-4">
            <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                    <h2 class="break-words text-lg font-semibold leading-snug">{{ $selected['display_name'] ?? $selected['key'] }}</h2>
                    <p class="mt-0.5 font-mono text-xs text-stone-400">{{ $selected['key'] }}</p>
                </div>
                @if ($selected['mismatch'])
                    <span class="flex shrink-0 items-center gap-1 rounded-full bg-red-500/10 px-2.5 py-0.5 text-xs font-medium text-red-600 dark:text-red-400">
                        <x-phosphor-warning-fill class="size-3" /> mismatch
                    </span>
                @else
                    <span class="flex shrink-0 items-center gap-1 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
                        <x-phosphor-check-fill class="size-3" /> in sync
                    </span>
                @endif
            </div>

            <div>
                <h3 class="mb-1 text-xs font-semibold uppercase tracking-wide text-stone-400">Description</h3>
                <p class="text-sm leading-relaxed text-stone-600 dark:text-stone-300">{{ trim($selected['description'] ?? '') !== '' ? $selected['description'] : '(no description in SKILL.md frontmatter)' }}</p>
            </div>

            <div>
                <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-stone-400">Agents · {{ count($selected['presence']) }}</h3>
                <div class="flex flex-col divide-y divide-stone-100 rounded-lg border border-stone-200 dark:divide-stone-800 dark:border-stone-700">
                    @foreach ($selected['presence'] as $p)
                        <div class="flex items-center gap-2 px-3 py-2 text-xs">
                            <span @class([
                                'w-16 shrink-0 rounded px-1.5 py-0.5 text-center font-mono font-medium',
                                'bg-accent-500/15 text-accent-700 dark:text-accent-300' => $p['agent'] === 'agents',
                                'bg-stone-100 text-stone-600 dark:bg-stone-800 dark:text-stone-300' => $p['agent'] !== 'agents',
                            ])>{{ $p['agent'] }}</span>
                            <span class="min-w-0 flex-1 truncate font-mono text-stone-500 dark:text-stone-400" title="{{ $p['path'] }}">
                                {{ $p['path'] }}
                                @if ($p['is_symlink'])
                                    <span class="text-accent-600 dark:text-accent-400"> → {{ $p['symlink_target'] }}</span>
                                @endif
                            </span>
                            <span class="shrink-0 font-mono text-stone-400">{{ $shortHash($p['content_hash'] ?? null) }}</span>
                        </div>
                    @endforeach
                </div>
            </div>

            <div class="mt-auto flex items-center justify-between gap-2 border-t border-stone-100 pt-4 dark:border-stone-800">
                <form method="POST" action="{{ route('sync.plan') }}">
                    @csrf
                    <input type="hidden" name="scope" value="{{ $scope }}">
                    <input type="hidden" name="target" value="skills">
                    <input type="hidden" name="kind" value="skills">
                    <input type="hidden" name="key" value="{{ $selected['key'] }}">
                    <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                    <button type="submit" class="btn-primary inline-flex items-center gap-1.5 rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">
                        <x-phosphor-arrows-left-right class="size-3.5" /> Sync this
                    </button>
                </form>
                <div class="flex items-center gap-1">
                    <form method="POST" action="{{ route('skills.remove') }}">
                        @csrf
                        <input type="hidden" name="scope" value="{{ $scope }}">
                        <input type="hidden" name="key" value="{{ $selected['key'] }}">
                        <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
                        <button type="submit" data-confirm class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 hover:text-red-600 dark:text-stone-400 dark:hover:bg-stone-800 dark:hover:text-red-400">
                            Unlink
                        </button>
                    </form>
                    <form method="POST" action="{{ route('skills.remove') }}">
                        @csrf
                        <input type="hidden" name="scope" value="{{ $scope }}">
                        <input type="hidden" name="key" value="{{ $selected['key'] }}">
                        <input type="hidden" name="purge" value="1">
                        <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
                        <button type="submit" data-confirm data-armed-text="Purge?" class="rounded-full px-3 py-1.5 text-sm font-medium text-red-600 transition hover:bg-red-500/10 dark:text-red-400">
                            Purge
                        </button>
                    </form>
                </div>
            </div>
        </div>
    @else
        @php
            $pref = collect($selected['presence'])->firstWhere('agent', 'agents') ?? ($selected['presence'][0] ?? null);
        @endphp
        <div class="flex flex-col gap-4">
            <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                    <h2 class="break-words text-lg font-semibold leading-snug">{{ $selected['key'] }}</h2>
                    <p class="mt-0.5 font-mono text-xs text-stone-400">mcp server</p>
                </div>
                @if ($selected['mismatch'])
                    <span class="flex shrink-0 items-center gap-1 rounded-full bg-red-500/10 px-2.5 py-0.5 text-xs font-medium text-red-600 dark:text-red-400">
                        <x-phosphor-warning-fill class="size-3" /> mismatch
                    </span>
                @else
                    <span class="flex shrink-0 items-center gap-1 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
                        <x-phosphor-check-fill class="size-3" /> in sync
                    </span>
                @endif
            </div>

            @if ($pref)
                <div>
                    <h3 class="mb-1 text-xs font-semibold uppercase tracking-wide text-stone-400">Summary</h3>
                    <dl class="grid grid-cols-[90px_1fr] gap-y-1 text-sm">
                        <dt class="text-stone-400">transport</dt>
                        <dd class="font-mono">{{ $pref['normalized']['transport'] ?? '-' }}</dd>
                        @if (! empty($pref['normalized']['command']))
                            <dt class="text-stone-400">command</dt>
                            <dd class="break-all font-mono text-xs">{{ implode(' ', array_merge($pref['normalized']['command'], $pref['normalized']['args'] ?? [])) }}</dd>
                        @endif
                        @if (! empty($pref['normalized']['url']))
                            <dt class="text-stone-400">url</dt>
                            <dd class="break-all font-mono text-xs">{{ $pref['normalized']['url'] }}</dd>
                        @endif
                        @if (array_key_exists('enabled', $pref['normalized']))
                            <dt class="text-stone-400">enabled</dt>
                            <dd class="font-mono">{{ $pref['normalized']['enabled'] ? 'true' : 'false' }}</dd>
                        @endif
                    </dl>
                </div>
            @endif

            <div>
                <h3 class="mb-2 text-xs font-semibold uppercase tracking-wide text-stone-400">Agents · {{ count($selected['presence']) }}</h3>
                <div class="flex flex-col divide-y divide-stone-100 rounded-lg border border-stone-200 dark:divide-stone-800 dark:border-stone-700">
                    @foreach ($selected['presence'] as $p)
                        <div class="flex items-center gap-2 px-3 py-2 text-xs">
                            <span @class([
                                'w-16 shrink-0 rounded px-1.5 py-0.5 text-center font-mono font-medium',
                                'bg-accent-500/15 text-accent-700 dark:text-accent-300' => $p['agent'] === 'agents',
                                'bg-stone-100 text-stone-600 dark:bg-stone-800 dark:text-stone-300' => $p['agent'] !== 'agents',
                            ])>{{ $p['agent'] }}</span>
                            <span class="min-w-0 flex-1">
                                <span class="block truncate font-mono text-stone-500 dark:text-stone-400" title="{{ $p['path'] }}">{{ $p['path'] }}</span>
                                @if (! empty($p['normalized']['env_keys']))
                                    <span class="block truncate text-stone-400">env: {{ implode(', ', $p['normalized']['env_keys']) }}</span>
                                @endif
                            </span>
                            <span class="shrink-0 font-mono text-stone-400">{{ $shortHash($p['fingerprint'] ?? null) }}</span>
                        </div>
                    @endforeach
                </div>
            </div>

            <div class="mt-auto flex items-center justify-between gap-2 border-t border-stone-100 pt-4 dark:border-stone-800">
                <form method="POST" action="{{ route('sync.plan') }}">
                    @csrf
                    <input type="hidden" name="scope" value="{{ $scope }}">
                    <input type="hidden" name="target" value="mcp">
                    <input type="hidden" name="kind" value="mcp">
                    <input type="hidden" name="key" value="{{ $selected['key'] }}">
                    <input type="hidden" name="return_to" value="{{ request()->fullUrl() }}">
                    <button type="submit" class="btn-primary inline-flex items-center gap-1.5 rounded-full bg-stone-900 px-4 py-1.5 text-sm font-medium text-white transition hover:bg-stone-700 active:scale-[0.97] dark:bg-stone-100 dark:text-stone-900 dark:hover:bg-white">
                        <x-phosphor-arrows-left-right class="size-3.5" /> Sync this
                    </button>
                </form>
                <form method="POST" action="{{ route('mcp.remove') }}">
                    @csrf
                    <input type="hidden" name="scope" value="{{ $scope }}">
                    <input type="hidden" name="key" value="{{ $selected['key'] }}">
                    <input type="hidden" name="return_to" value="{{ route('dashboard', ['scope' => $scope]) }}">
                    <button type="submit" data-confirm class="rounded-full px-3 py-1.5 text-sm text-stone-500 transition hover:bg-stone-100 hover:text-red-600 dark:text-stone-400 dark:hover:bg-stone-800 dark:hover:text-red-400">
                        Remove
                    </button>
                </form>
            </div>
        </div>
    @endif

@else
    <div class="flex h-full flex-col items-center justify-center gap-3 text-center">
        <span class="flex size-14 items-center justify-center rounded-2xl bg-stone-200/70 text-stone-400 dark:bg-stone-800 dark:text-stone-500">
            <x-phosphor-cursor-click class="size-7" />
        </span>
        <p class="text-sm text-stone-500 dark:text-stone-400">
            Select a skill or MCP to see agents, hashes and actions.<br>
            Use <x-phosphor-plus class="inline size-3.5 align-text-bottom" /> to install, <x-phosphor-arrows-left-right class="inline size-3.5 align-text-bottom" /> to sync.
        </p>
    </div>
@endif
