<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Symfony\Component\Process\Process;
use Tests\TestCase;

/**
 * Runs downloads and previews against a real HTTP server (PHP's built-in
 * one standing in for an agent) instead of Http::fake(), which skips
 * Guzzle's handlers. The first streaming build passed every faked test but
 * failed on every real request: the 'stream' option picks Guzzle's
 * PHP-stream handler, and that handler rejects cURL options.
 */
class AgentStreamRealServerTest extends TestCase
{
    use RefreshDatabase;

    private Process $server;

    private int $port;

    private string $docroot;

    protected function setUp(): void
    {
        parent::setUp();

        $this->docroot = sys_get_temp_dir().'/lanhub-fake-agent-'.bin2hex(random_bytes(4));
        mkdir($this->docroot);
        file_put_contents($this->docroot.'/router.php', <<<'PHP'
            <?php
            if (($_SERVER['HTTP_AUTHORIZATION'] ?? '') !== 'Bearer test-token') {
                http_response_code(401);
                header('Content-Type: application/json');
                echo '{"message":"bad token"}';
                return true;
            }
            $size = (int) preg_replace('/\\D/', '', $_GET['path'] ?? '');
            if ($size === 0) {
                http_response_code(404);
                header('Content-Type: application/json');
                echo '{"message":"not a file"}';
                return true;
            }
            header('Content-Type: '.(str_contains($_SERVER['REQUEST_URI'], '/api/preview') ? 'image/png' : 'application/octet-stream'));
            header('Content-Length: '.$size);
            $block = str_repeat('0123456789abcdef', 4096);
            for ($sent = 0; $sent < $size; $sent += strlen($chunk)) {
                $chunk = substr($block, 0, min(strlen($block), $size - $sent));
                echo $chunk;
                flush();
            }
            return true;
            PHP);

        $socket = stream_socket_server('tcp://127.0.0.1:0');
        $this->port = (int) substr(strrchr(stream_socket_get_name($socket, false), ':'), 1);
        fclose($socket);

        $this->server = new Process([PHP_BINARY, '-S', "127.0.0.1:{$this->port}", $this->docroot.'/router.php']);
        $this->server->start();

        for ($i = 0; $i < 50 && ! @fsockopen('127.0.0.1', $this->port); $i++) {
            usleep(100_000);
        }
    }

    protected function tearDown(): void
    {
        $this->server->stop(0);
        @unlink($this->docroot.'/router.php');
        @rmdir($this->docroot);

        parent::tearDown();
    }

    private function machineFor(User $user): Machine
    {
        $machine = Machine::factory()->create([
            'host' => '127.0.0.1',
            'port' => $this->port,
            'use_tls' => false,
            'agent_token' => 'test-token',
        ]);
        $machine->users()->attach($user, ['role' => 'read_only']);

        return $machine;
    }

    public function test_a_real_download_streams_through_intact(): void
    {
        $size = 5 * 1024 * 1024 + 123;
        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        // The fake agent serves as many bytes as the path's digits say.
        $response = $this->actingAs($user)->get("/machines/{$machine->id}/download?path=size-{$size}.bin");

        $response->assertOk();
        $response->assertHeader('Content-Length', (string) $size);
        $body = $response->streamedContent();
        $this->assertSame($size, strlen($body));
        $this->assertSame(str_repeat('0123456789abcdef', 2), substr($body, 0, 32));
    }

    public function test_a_real_preview_streams_through_with_its_content_type(): void
    {
        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/preview?path=size-70000.png");

        $response->assertOk();
        $response->assertHeader('Content-Type', 'image/png');
        $this->assertSame(70000, strlen($response->streamedContent()));
    }

    public function test_a_real_agent_error_is_mapped(): void
    {
        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user)->get("/machines/{$machine->id}/download?path=missing.bin")->assertNotFound();
    }
}
