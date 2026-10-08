<?php

namespace Tests\Feature;

use App\Models\CommandLog;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class SyncaTest extends TestCase
{
    use RefreshDatabase;

    private string $stubLog;

    protected function setUp(): void
    {
        parent::setUp();
        $this->stubLog = tempnam(sys_get_temp_dir(), 'synca-stub-');
        putenv('SYNCA_BIN='.base_path('tests/fixtures/synca-stub.sh'));
        putenv('SYNCA_STUB_LOG='.$this->stubLog);
        $_ENV['SYNCA_BIN'] = base_path('tests/fixtures/synca-stub.sh');
        $_SERVER['SYNCA_BIN'] = base_path('tests/fixtures/synca-stub.sh');
        $_ENV['SYNCA_STUB_LOG'] = $this->stubLog;
        $_SERVER['SYNCA_STUB_LOG'] = $this->stubLog;
    }

    protected function tearDown(): void
    {
        putenv('SYNCA_BIN');
        putenv('SYNCA_STUB_LOG');
        unset($_ENV['SYNCA_BIN'], $_SERVER['SYNCA_BIN'], $_ENV['SYNCA_STUB_LOG'], $_SERVER['SYNCA_STUB_LOG']);
        @unlink($this->stubLog);
        parent::tearDown();
    }

    private function stubCalls(): string
    {
        return is_file($this->stubLog) ? file_get_contents($this->stubLog) : '';
    }

    public function test_dashboard_lists_skills_and_mcps_with_log_column(): void
    {
        $this->get('/')
            ->assertOk()
            ->assertSee('demo')
            ->assertSee('demo-mcp')
            ->assertSee('demo-conflict')
            ->assertSee('logs-column');
    }

    public function test_selecting_a_skill_shows_its_detail(): void
    {
        $this->get('/?section=skills&key=demo')
            ->assertOk()
            ->assertSee('A demo skill.')
            ->assertSee('Sync this')
            ->assertSee('Purge');
    }

    public function test_sync_plan_runs_dry_run_and_flashes_plan_with_conflicts(): void
    {
        $this->post('/sync/plan', [
            'scope' => 'user',
            'target' => 'skills',
        ])->assertRedirect();

        $this->assertSame(1, CommandLog::count());
        $log = CommandLog::first();
        $this->assertSame('dry-run', $log->status);
        $this->assertStringContainsString('sync skills', $log->command);

        $this->get('/')->assertOk()
            ->assertSee('conflict(s)', false)
            ->assertSee('demo-conflict');
    }

    public function test_sync_apply_runs_per_key_resolution_then_main_sync(): void
    {
        $this->post('/sync/apply', [
            'scope' => 'user',
            'target' => 'skills',
            'decisions' => json_encode(['skills' => ['demo-conflict' => 'keep-source'], 'mcps' => []]),
        ])->assertRedirect();

        $calls = $this->stubCalls();
        $this->assertStringContainsString('sync skills --scope user --on-conflict keep-source --key demo-conflict', $calls);
        $this->assertStringContainsString('sync skills --scope user --on-conflict skip', $calls);
        $this->assertSame(2, CommandLog::count());
    }

    public function test_install_skill_preview_then_apply(): void
    {
        $this->post('/skills/install', [
            'scope' => 'user',
            'source' => '/tmp/some-skill',
        ])->assertRedirect();

        $this->assertStringContainsString("skills install /tmp/some-skill --scope user --dry-run", $this->stubCalls());
        $this->assertSame('dry-run', CommandLog::first()->status);

        $this->post('/skills/install', [
            'scope' => 'user',
            'source' => '/tmp/some-skill',
            'apply' => '1',
        ])->assertRedirect();

        $this->assertStringContainsString("skills install /tmp/some-skill --scope user", $this->stubCalls());
        $this->assertSame('ok', CommandLog::latest('id')->first()->status);
    }

    public function test_remove_skill_purges_with_yes_flag(): void
    {
        $this->post('/skills/remove', [
            'scope' => 'user',
            'key' => 'demo',
            'purge' => '1',
        ])->assertRedirect();

        $this->assertStringContainsString('skills remove demo --scope user --purge --yes', $this->stubCalls());
    }

    public function test_add_mcp_validates_name_and_posts(): void
    {
        $this->post('/mcp/add', [
            'scope' => 'user',
            'name' => 'bad name!',
            'transport' => 'stdio',
            'command' => 'x',
        ])->assertSessionHasErrors('name');

        $this->post('/mcp/add', [
            'scope' => 'user',
            'name' => 'demo-mcp-2',
            'transport' => 'stdio',
            'command' => 'npx -y @pkg/server',
            'apply' => '1',
        ])->assertRedirect();

        $this->assertStringContainsString('mcp add demo-mcp-2 --transport stdio --scope user --command npx -y @pkg/server', $this->stubCalls());
    }

    public function test_remove_mcp(): void
    {
        $this->post('/mcp/remove', ['scope' => 'user', 'key' => 'demo-mcp'])->assertRedirect();
        $this->assertStringContainsString('mcp remove demo-mcp --scope user', $this->stubCalls());
    }

    public function test_update_check_flashes_result(): void
    {
        $this->post('/update/check')->assertRedirect();
        $this->get('/')->assertOk()->assertSee('update available');
        $this->assertStringContainsString('update --check --json', $this->stubCalls());
    }

    public function test_logs_feed_paginates_before_and_after(): void
    {
        CommandLog::create(['scope' => 'user', 'command' => 'synca one', 'status' => 'ok', 'exit_code' => 0]);
        $newer = CommandLog::create(['scope' => 'user', 'command' => 'synca two', 'status' => 'ok', 'exit_code' => 0]);

        $this->get('/logs')->assertOk()->assertSeeInOrder(['synca two', 'synca one']);
        $this->get('/logs?before='.$newer->id)->assertOk()->assertSee('synca one')->assertDontSee('synca two');
        $this->get('/logs?after='.($newer->id - 1))->assertOk()->assertSee('synca two')->assertDontSee('synca one');
    }

    public function test_clear_logs(): void
    {
        CommandLog::create(['scope' => 'user', 'command' => 'synca x', 'status' => 'ok', 'exit_code' => 0]);
        $this->post('/logs/clear')->assertRedirect();
        $this->assertSame(0, CommandLog::count());
    }
}
