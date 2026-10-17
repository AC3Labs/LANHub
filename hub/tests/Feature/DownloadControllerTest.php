<?php

namespace Tests\Feature;

use App\Http\Controllers\DownloadController;
use App\Models\Machine;
use App\Models\User;
use GuzzleHttp\Promise\Create;
use GuzzleHttp\Psr7\FnStream;
use GuzzleHttp\Psr7\Response as Psr7Response;
use GuzzleHttp\Psr7\Utils;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;
use Tests\TestCase;

class DownloadControllerTest extends TestCase
{
    use RefreshDatabase;

    private function machineFor(User $user, string $role = 'read_only'): Machine
    {
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => $role]);

        return $machine;
    }

    public function test_it_streams_the_file_with_download_headers(): void
    {
        Http::fake([
            '*/api/download*' => Http::response('file contents', 200, ['Content-Length' => '13', 'Content-Type' => 'application/octet-stream']),
        ]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/download?path=".urlencode('C:\Users\me\report.pdf'));

        $response->assertOk();
        $this->assertSame('file contents', $response->streamedContent());
        $response->assertHeader('Content-Type', 'application/octet-stream');
        $response->assertHeader('Content-Length', '13');
        $response->assertHeader('Content-Disposition', 'attachment; filename=report.pdf');
        $response->assertHeader('X-Content-Type-Options', 'nosniff');
        $response->assertHeader('X-Accel-Buffering', 'no');

        Http::assertSent(fn ($request) => str_contains($request->url(), '/api/download')
            && $request['path'] === 'C:\Users\me\report.pdf');
    }

    public function test_a_non_ascii_filename_gets_an_ascii_fallback_and_a_utf8_name(): void
    {
        Http::fake(['*/api/download*' => Http::response('x', 200)]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/download?path=".urlencode('/home/me/café "menu".txt'));

        $disposition = $response->headers->get('Content-Disposition');
        $this->assertStringContainsString('filename="caf_ _menu_.txt"', $disposition);
        $this->assertStringContainsString("filename*=utf-8''caf%C3%A9%20%22menu%22.txt", $disposition);
    }

    public function test_it_denies_a_user_with_no_access_to_the_machine(): void
    {
        Http::fake();
        $user = User::factory()->create();
        $machine = Machine::factory()->create();

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/download?path=/notes.txt")
            ->assertForbidden();

        Http::assertNothingSent();
    }

    public function test_an_admin_without_a_grant_is_denied_too(): void
    {
        Http::fake();
        $admin = User::factory()->create(['is_admin' => true]);
        $machine = Machine::factory()->create();

        $this->actingAs($admin)
            ->get("/machines/{$machine->id}/download?path=/notes.txt")
            ->assertForbidden();
    }

    public function test_a_guest_is_sent_to_login(): void
    {
        $machine = Machine::factory()->create();

        $this->get("/machines/{$machine->id}/download?path=/notes.txt")
            ->assertRedirect('/login');
    }

    public function test_a_missing_path_is_a_400(): void
    {
        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user)->get("/machines/{$machine->id}/download")->assertStatus(400);
        $this->actingAs($user)->get("/machines/{$machine->id}/download?path[]=x")->assertStatus(400);
    }

    public function test_a_missing_file_on_the_agent_is_a_404(): void
    {
        Http::fake(['*/api/download*' => Http::response(['message' => '/gone.txt is not a file.'], 404)]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/download?path=/gone.txt")
            ->assertNotFound();
    }

    public function test_an_agent_server_error_is_a_502(): void
    {
        Http::fake(['*/api/download*' => Http::response(['message' => 'boom'], 500)]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/download?path=/a.txt")
            ->assertStatus(502);
    }

    public function test_an_unreachable_agent_is_a_502(): void
    {
        Http::fake(fn () => throw new ConnectionException('Connection refused'));

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/download?path=/a.txt")
            ->assertStatus(502);
    }

    public function test_a_file_cut_short_by_the_agent_keeps_the_promised_length_and_is_logged(): void
    {
        Log::spy();
        Http::fake(['*/api/download*' => Http::response('only half', 200, ['Content-Length' => '18'])]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/download?path=/a.txt");

        // The browser compares the bytes it got with this header and marks
        // the download failed, rather than saving a short file as complete.
        $response->assertHeader('Content-Length', '18');
        $this->assertSame('only half', $response->streamedContent());
        Log::shouldHaveReceived('warning')->withArgs(fn ($m) => str_contains($m, 'after 9 of 18 bytes'));
    }

    public function test_a_stream_that_breaks_mid_file_stops_quietly_and_is_logged(): void
    {
        Log::spy();
        $reads = 0;
        $body = FnStream::decorate(Utils::streamFor(''), [
            'eof' => fn () => false,
            'read' => function () use (&$reads) {
                if ($reads++ === 0) {
                    return 'first chunk';
                }

                throw new \RuntimeException('connection reset by agent');
            },
        ]);
        Http::fake(['*/api/download*' => fn () => Create::promiseFor(new Psr7Response(200, ['Content-Length' => '100'], $body))]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/download?path=/a.txt");

        $this->assertSame('first chunk', $response->streamedContent());
        Log::shouldHaveReceived('warning')->withArgs(fn ($m) => str_contains($m, 'broke after 11 bytes'));
    }

    public function test_a_large_file_streams_without_growing_hub_memory(): void
    {
        $size = 64 * 1024 * 1024;
        $file = tmpfile();
        ftruncate($file, $size);
        $body = Utils::streamFor($file);
        Http::fake(['*/api/download*' => fn () => Create::promiseFor(new Psr7Response(200, ['Content-Length' => (string) $size], $body))]);

        $user = User::factory()->create();
        $machine = $this->machineFor($user);

        $this->actingAs($user);
        $response = $this->app->make(DownloadController::class)
            ->show(request()->merge(['path' => '/big.bin']), $machine);

        $sent = 0;
        memory_reset_peak_usage();
        $before = memory_get_usage();
        ob_start(function (string $buffer) use (&$sent) {
            $sent += strlen($buffer);

            return '';
        }, 1024 * 1024);
        $response->sendContent();
        ob_end_clean();

        $this->assertSame($size, $sent);
        $this->assertLessThan(8 * 1024 * 1024, memory_get_peak_usage() - $before);
    }
}
