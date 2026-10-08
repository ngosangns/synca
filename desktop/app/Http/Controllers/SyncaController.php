<?php

namespace App\Http\Controllers;

use App\Models\CommandLog;
use App\Services\Synca;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\View\View;

class SyncaController extends Controller
{
    private const LOG_PAGE = 30;

    public function index(Request $request): View
    {
        $scope = Synca::normalizeScope($request->query('scope'));
        $section = $request->query('section') === 'mcps' ? 'mcps' : 'skills';
        $key = (string) $request->query('key', '');
        $panel = (string) $request->query('panel', '');
        $available = Synca::available();

        $skills = $available ? Synca::listJson('skills', $scope) : [];
        $mcps = $available ? Synca::listJson('mcp', $scope) : [];

        $selected = null;
        if ($key !== '') {
            $pool = $section === 'skills' ? $skills : $mcps;
            $selected = collect($pool)->firstWhere('key', $key);
        }

        $logs = CommandLog::latest('id')->limit(self::LOG_PAGE)->get();

        $data = [
            'scope' => $scope,
            'section' => $section,
            'key' => $key,
            'panel' => $panel,
            'available' => $available,
            'skills' => $skills,
            'mcps' => $mcps,
            'selected' => $selected,
            'logs' => $logs,
            'projectDir' => Synca::projectDir(),
            'plan' => session('plan'),
            'update' => session('update'),
            'bin' => Synca::bin(),
        ];

        return $request->boolean('partial')
            ? view('dashboard._boards', $data)
            : view('dashboard.index', $data);
    }

    public function plan(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'target' => ['required', 'in:skills,mcp,all'],
            'kind' => ['nullable', 'in:skills,mcp'],
            'key' => ['nullable', 'string', 'max:255'],
            'return_to' => ['nullable', 'string'],
        ]);

        $args = ['sync', $validated['target'], '--scope', $validated['scope'], '--dry-run'];
        if ($validated['scope'] === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }
        if (! empty($validated['key'])) {
            array_push($args, '--key', $validated['key']);
        }

        $result = Synca::runLogged($args, $validated['scope'], 'dry-run');

        return redirect($this->back($request))->with('plan', [
            'title' => "sync {$validated['target']}".(! empty($validated['key']) ? " · {$validated['key']}" : ''),
            'output' => $result['output'],
            'ok' => $result['ok'],
            'summary' => Synca::planSummary($result['output']),
            'conflicts' => Synca::conflictsInOutput($result['output']),
            'scope' => $validated['scope'],
            'target' => $validated['target'],
            'kind' => $validated['kind'] ?? $validated['target'],
            'key' => $validated['key'] ?? null,
        ]);
    }

    public function apply(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'target' => ['required', 'in:skills,mcp,all'],
            'key' => ['nullable', 'string', 'max:255'],
            'decisions' => ['nullable', 'string'],
        ]);
        $scope = $validated['scope'];
        $messages = [];

        // Per-conflict policies resolved as sequential per-key syncs.
        $decisions = json_decode($validated['decisions'] ?? '{}', true) ?: [];
        foreach (['skills' => 'skills', 'mcps' => 'mcp'] as $bag => $kind) {
            foreach (($decisions[$bag] ?? []) as $conflictKey => $policy) {
                if (! in_array($policy, ['keep-source', 'keep-target'], true)) {
                    continue;
                }
                $r = $this->syncRun($scope, $kind, (string) $conflictKey, $policy);
                $messages[] = $r['ok'] ? "resolved {$kind} '{$conflictKey}' ({$policy})" : "FAILED {$kind} '{$conflictKey}'";
            }
        }

        // Everything else applies with conflicts skipped (already resolved
        // keys come back skip_same; user-skipped ones stay conflicts).
        $r = $this->syncRun($scope, $validated['target'], $validated['key'] ?? null, 'skip');
        $messages[] = $r['ok'] ? 'sync applied' : 'sync failed';

        return redirect($this->back($request))->with('status', implode(' · ', $messages));
    }

    private function syncRun(string $scope, string $target, ?string $key, string $policy): array
    {
        $args = ['sync', $target, '--scope', $scope, '--on-conflict', $policy];
        if ($scope === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }
        if ($key && $target !== 'all') {
            array_push($args, '--key', $key);
        }

        return Synca::runLogged($args, $scope, 'ok');
    }

    public function installSkill(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'source' => ['required', 'string', 'max:2000'],
            'apply' => ['nullable', 'boolean'],
        ]);
        $scope = $validated['scope'];

        $args = ['skills', 'install', $validated['source'], '--scope', $scope];
        if ($scope === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }

        if (! $request->boolean('apply')) {
            $result = Synca::runLogged([...$args, '--dry-run'], $scope, 'dry-run');

            return redirect($this->back($request))->with('plan', [
                'title' => "install skill · {$validated['source']}",
                'output' => $result['output'],
                'ok' => $result['ok'],
                'summary' => Synca::planSummary($result['output']),
                'conflicts' => [],
                'install_source' => $validated['source'],
                'scope' => $scope,
            ]);
        }

        $result = Synca::runLogged($args, $scope, 'ok');

        return redirect($this->back($request))->with(
            'status',
            $result['ok'] ? 'Skill installed.' : 'Install failed — see log pane.'
        );
    }

    public function removeSkill(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'key' => ['required', 'string', 'max:255'],
            'purge' => ['nullable', 'boolean'],
        ]);
        $scope = $validated['scope'];
        $purge = $request->boolean('purge');

        $args = ['skills', 'remove', $validated['key'], '--scope', $scope];
        if ($scope === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }
        if ($purge) {
            array_push($args, '--purge', '--yes');
        }

        $result = Synca::runLogged($args, $scope, 'ok');

        return redirect($this->back($request))->with(
            'status',
            $result['ok']
                ? ($purge ? "Purged '{$validated['key']}'." : "Unlinked '{$validated['key']}'.")
                : 'Remove failed — see log pane.'
        );
    }

    public function addMcp(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'name' => ['required', 'string', 'max:120', 'regex:/^[A-Za-z0-9._ -]+$/'],
            'transport' => ['required', 'in:stdio,http,sse,local'],
            'command' => ['nullable', 'string', 'max:2000', 'required_if:transport,stdio,local'],
            'url' => ['nullable', 'string', 'max:2000', 'required_if:transport,http,sse'],
            'enabled' => ['nullable', 'boolean'],
            'apply' => ['nullable', 'boolean'],
        ]);
        $scope = $validated['scope'];

        $args = ['mcp', 'add', $validated['name'], '--transport', $validated['transport'], '--scope', $scope];
        if ($scope === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }
        if (! empty($validated['command'])) {
            array_push($args, '--command', $validated['command']);
        }
        if (! empty($validated['url'])) {
            array_push($args, '--url', $validated['url']);
        }
        if ($request->has('enabled')) {
            array_push($args, '--enabled', $request->boolean('enabled') ? 'true' : 'false');
        }

        if (! $request->boolean('apply')) {
            $result = Synca::runLogged([...$args, '--dry-run'], $scope, 'dry-run');

            return redirect($this->back($request))->with('plan', [
                'title' => "add mcp · {$validated['name']}",
                'output' => $result['output'],
                'ok' => $result['ok'],
                'summary' => Synca::planSummary($result['output']),
                'conflicts' => [],
                'mcp_payload' => $request->only('name', 'transport', 'command', 'url', 'enabled'),
                'scope' => $scope,
            ]);
        }

        $result = Synca::runLogged($args, $scope, 'ok');

        return redirect($this->back($request))->with(
            'status',
            $result['ok'] ? "MCP '{$validated['name']}' added." : 'Add failed — see log pane.'
        );
    }

    public function removeMcp(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'scope' => ['required', 'in:user,project'],
            'key' => ['required', 'string', 'max:255'],
        ]);
        $scope = $validated['scope'];

        $args = ['mcp', 'remove', $validated['key'], '--scope', $scope];
        if ($scope === 'project') {
            array_push($args, '--cwd', Synca::projectDir());
        }

        $result = Synca::runLogged($args, $scope, 'ok');

        return redirect($this->back($request))->with(
            'status',
            $result['ok'] ? "MCP '{$validated['key']}' removed." : 'Remove failed — see log pane.'
        );
    }

    public function updateCheck(Request $request): RedirectResponse
    {
        $result = Synca::runLogged(['update', '--check', '--json'], 'user', 'ok');
        $info = json_decode(trim($result['output']), true);

        return redirect($this->back($request))->with('update', is_array($info) ? $info : [
            'ok' => false,
            'message' => trim($result['output']) ?: 'update check failed',
        ]);
    }

    public function updateInstall(Request $request): RedirectResponse
    {
        $result = Synca::runLogged(['update', '--json'], 'user', 'ok');
        $info = json_decode(trim($result['output']), true);
        $message = is_array($info) ? ($info['message'] ?? 'update finished') : 'update failed — see log pane';

        return redirect($this->back($request))->with('status', $message);
    }

    public function projectDir(Request $request): RedirectResponse
    {
        $validated = $request->validate([
            'dir' => ['required', 'string', 'max:500'],
        ]);
        $dir = $validated['dir'];
        if (! is_dir($dir)) {
            return redirect($this->back($request))->with('status', "Not a directory: {$dir}");
        }
        Synca::setProjectDir($dir);

        return redirect($this->back($request))->with('status', 'Project dir saved.');
    }

    public function clearLogs(Request $request): RedirectResponse
    {
        CommandLog::truncate();

        return redirect($this->back($request))->with('status', 'Log cleared.');
    }

    /**
     * Where to send the user after a POST: keeps scope/section/selection.
     */
    private function back(Request $request): string
    {
        $returnTo = (string) $request->input('return_to', '');
        if ($returnTo !== '' && str_starts_with($returnTo, '/')) {
            return $returnTo;
        }

        return route('dashboard', [
            'scope' => $request->input('scope', 'user'),
            'section' => $request->input('section', 'skills'),
            'key' => $request->input('key'),
        ]);
    }
}
