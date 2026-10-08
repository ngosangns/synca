<?php

namespace App\Services;

use App\Models\CommandLog;
use Illuminate\Support\Facades\Cache;
use Symfony\Component\Process\Process;

class Synca
{
    public const SCOPES = ['user', 'project'];

    /**
     * Resolve the synca binary: SYNCA_BIN env, ~/.local/bin/synca, else PATH.
     */
    public static function bin(): string
    {
        $bin = env('SYNCA_BIN');
        if ($bin && is_executable($bin)) {
            return $bin;
        }
        $home = getenv('HOME') ?: ($_SERVER['HOME'] ?? '');
        if ($home && is_executable("{$home}/.local/bin/synca")) {
            return "{$home}/.local/bin/synca";
        }

        return 'synca';
    }

    public static function available(): bool
    {
        return self::probe() === true;
    }

    private static function probe(): bool
    {
        static $ok;
        if ($ok !== null) {
            return $ok;
        }
        $p = new Process([self::bin(), '--version']);
        $p->setTimeout(5);
        try {
            $p->run();
            $ok = $p->isSuccessful();
        } catch (\Throwable) {
            $ok = false;
        }

        return $ok;
    }

    /**
     * Project-scope cwd: user-saved override, else the repo containing this app.
     */
    public static function projectDir(): string
    {
        return Cache::get('synca.project_dir')
            ?: env('SYNCA_PROJECT_DIR')
            ?: dirname(base_path());
    }

    public static function setProjectDir(string $dir): void
    {
        Cache::forever('synca.project_dir', $dir);
    }

    public static function normalizeScope(?string $scope): string
    {
        return in_array($scope, self::SCOPES, true) ? $scope : 'user';
    }

    /**
     * Common CLI args for a scope (project needs --cwd).
     */
    private static function scopeArgs(string $scope): array
    {
        return $scope === 'project' ? ['--scope', 'project', '--cwd', self::projectDir()] : ['--scope', 'user'];
    }

    /**
     * @return array<int, array> decoded list entries
     */
    public static function listJson(string $kind, string $scope): array
    {
        $result = self::raw([$kind, 'list', ...self::scopeArgs($scope), '--json']);
        $data = json_decode($result['output'] ?? '', true);

        return is_array($data) ? $data : [];
    }

    /**
     * Run a command without logging it (read-only calls).
     *
     * @return array{ok: bool, exit: int, output: string}
     */
    public static function raw(array $args, ?int $timeout = 30): array
    {
        $process = new Process([self::bin(), ...$args]);
        $process->setTimeout($timeout);
        try {
            $process->run();
        } catch (\Throwable $e) {
            return ['ok' => false, 'exit' => -1, 'output' => $e->getMessage()];
        }

        return [
            'ok' => $process->isSuccessful(),
            'exit' => $process->getExitCode() ?? -1,
            'output' => $process->getOutput().$process->getErrorOutput(),
        ];
    }

    /**
     * Run a mutating (or dry-run) command and persist it to the log pane.
     *
     * @return array{ok: bool, exit: int, output: string, log: CommandLog}
     */
    public static function runLogged(array $args, string $scope, string $status): array
    {
        $result = self::raw($args, 120);
        $display = 'synca '.implode(' ', array_map(
            fn (string $a) => preg_match('/[\s]/', $a) ? "'".str_replace("'", "\\'", $a)."'" : $a,
            $args
        ));

        $log = CommandLog::create([
            'scope' => $scope,
            'command' => $display,
            'status' => $result['ok'] ? $status : 'error',
            'exit_code' => $result['exit'],
            'output' => trim($result['output'] ?? ''),
        ]);

        return [...$result, 'log' => $log];
    }

    /**
     * Parse `{kind: "conflict_skill"|"conflict_mcp", ...}` actions out of a
     * printed plan so the UI can offer per-item conflict policies.
     *
     * @return array<int, array{kind: string, key: string, paths: array<int,string>}>
     */
    public static function conflictsInOutput(string $output): array
    {
        $conflicts = [];
        foreach (preg_split('/\r?\n/', $output) as $line) {
            if (! preg_match('/\d+\.\s*(\{.*\})\s*$/', trim($line), $m)) {
                continue;
            }
            $action = json_decode($m[1], true);
            if (! is_array($action)) {
                continue;
            }
            if (($action['kind'] ?? '') === 'conflict_skill') {
                $conflicts[] = ['kind' => 'skill', 'key' => $action['skill_key'] ?? '', 'paths' => $action['paths'] ?? []];
            } elseif (($action['kind'] ?? '') === 'conflict_mcp') {
                $conflicts[] = ['kind' => 'mcp', 'key' => $action['server'] ?? '', 'paths' => $action['fingerprints'] ?? []];
            }
        }

        return $conflicts;
    }

    /**
     * Count plan actions by kind (skip_same, link, conflict_skill, ...).
     *
     * @return array<string, int>
     */
    public static function planSummary(string $output): array
    {
        $counts = [];
        foreach (preg_split('/\r?\n/', $output) as $line) {
            if (! preg_match('/"kind"\s*:\s*"([a-z_]+)"/', $line, $m)) {
                continue;
            }
            $counts[$m[1]] = ($counts[$m[1]] ?? 0) + 1;
        }
        arsort($counts);

        return $counts;
    }
}
