<?php

namespace App\Services;

use App\Exceptions\AgentException;
use App\Models\Machine;
use Illuminate\Http\Client\PendingRequest;
use Illuminate\Http\Client\Response;
use Illuminate\Http\UploadedFile;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Str;

/**
 * Talks to the LANHub agent running on a registered Machine.
 *
 * @see docs/AGENT_API.md for the wire contract every agent must implement.
 */
class AgentClient
{
    public function __construct(private readonly Machine $machine)
    {
        //
    }

    public function health(): array
    {
        return $this->request()->get('/api/health')
            ->throw($this->throwHandler())
            ->json();
    }

    public function drives(): array
    {
        return $this->get('/api/drives')['drives'] ?? [];
    }

    /**
     * Cumulative bytes sent/received since the agent's network
     * interface(s) came up — not a rate. See computeCurrentSpeeds in
     * App\Livewire\Dashboard\Index for how two samples become a live
     * speed.
     *
     * @return array{bytes_sent: int, bytes_recv: int}
     */
    public function netstats(): array
    {
        $stats = $this->get('/api/netstats');

        return [
            'bytes_sent' => (int) ($stats['bytes_sent'] ?? 0),
            'bytes_recv' => (int) ($stats['bytes_recv'] ?? 0),
        ];
    }

    public function listDirectory(string $path): array
    {
        return $this->get('/api/list', ['path' => $path]);
    }

    public function download(string $path): Response
    {
        return $this->request()
            ->withOptions(['stream' => true])
            ->get('/api/download', ['path' => $path])
            ->throw($this->throwHandler());
    }

    public function upload(string $destinationDir, UploadedFile $file): array
    {
        return $this->request()
            ->attach('file', fopen($file->getRealPath(), 'r'), $file->getClientOriginalName())
            ->post('/api/upload?'.http_build_query(['path' => $destinationDir]))
            ->throw($this->throwHandler())
            ->json();
    }

    public function mkdir(string $path): array
    {
        return $this->post('/api/mkdir', ['path' => $path]);
    }

    public function rename(string $path, string $newName): array
    {
        return $this->post('/api/rename', ['path' => $path, 'new_name' => $newName]);
    }

    public function move(string $source, string $destination): array
    {
        return $this->post('/api/move', ['source' => $source, 'destination' => $destination]);
    }

    public function copy(string $source, string $destination): array
    {
        return $this->post('/api/copy', ['source' => $source, 'destination' => $destination]);
    }

    public function delete(string $path, bool $recursive = true): array
    {
        return $this->post('/api/delete', ['path' => $path, 'recursive' => $recursive]);
    }

    public function search(string $path, string $query): array
    {
        return $this->get('/api/search', ['path' => $path, 'query' => $query]);
    }

    public function preview(string $path): Response
    {
        return $this->request()
            ->withOptions(['stream' => true])
            ->get('/api/preview', ['path' => $path])
            ->throw($this->throwHandler());
    }

    /**
     * Walks a directory tree via repeated non-recursive GET /api/list calls
     * (the agent has no bulk/recursive listing endpoint) and returns a flat
     * list of files with a path relative to $root, using $separator for
     * both reading and rebuilding paths. Bounded by $maxEntries so a huge
     * tree can't make a sync rule run forever — matches the same
     * philosophy as the agent's own /api/search.
     *
     * @return array<int, array{relative: string, path: string, size: ?int, modified: ?string}>
     */
    public function listFilesRecursive(string $root, string $separator, int $maxEntries = 5000): array
    {
        $results = [];
        $queue = [$root];

        while ($queue !== [] && count($results) < $maxEntries) {
            $dir = array_shift($queue);

            try {
                $listing = $this->listDirectory($dir);
            } catch (AgentException) {
                continue;
            }

            foreach ($listing['entries'] as $entry) {
                if ($entry['type'] === 'dir') {
                    $queue[] = $entry['path'];

                    continue;
                }

                // Always normalized to "/" regardless of this machine's own
                // separator, so a relative path computed on a Windows
                // source can be matched against one from a Linux
                // destination (and vice versa) in RunSyncRules.
                $relative = ltrim(Str::after($entry['path'], rtrim($root, $separator)), $separator);
                $relative = str_replace($separator, '/', $relative);
                $results[] = [
                    'relative' => $relative,
                    'path' => $entry['path'],
                    'size' => $entry['size'],
                    'modified' => $entry['modified'],
                ];

                if (count($results) >= $maxEntries) {
                    break;
                }
            }
        }

        return $results;
    }

    public function activity(int $limit = 200): array
    {
        return $this->get('/api/activity', ['limit' => $limit])['entries'] ?? [];
    }

    public function createTransfer(string $source, string $destination, string $kind = 'copy'): array
    {
        return $this->post('/api/transfers', ['source' => $source, 'destination' => $destination, 'kind' => $kind]);
    }

    public function transfer(string $transferId): array
    {
        return $this->get("/api/transfers/{$transferId}");
    }

    public function transfers(): array
    {
        return $this->get('/api/transfers')['transfers'] ?? [];
    }

    private function get(string $uri, array $query = []): array
    {
        return $this->request()->get($uri, $query)->throw($this->throwHandler())->json();
    }

    private function post(string $uri, array $payload): array
    {
        return $this->request()->post($uri, $payload)->throw($this->throwHandler())->json();
    }

    private function throwHandler(): \Closure
    {
        return function (Response $response) {
            throw new AgentException(
                $response->json('message') ?? "Agent request to {$this->machine->name} failed with status {$response->status()}.",
                $response->status(),
            );
        };
    }

    private function request(): PendingRequest
    {
        $request = Http::baseUrl($this->machine->base_url)
            ->withToken($this->machine->agent_token)
            ->timeout(15);

        // Agents serve HTTPS with a self-signed cert (see docs/AGENT_API.md
        // and agent/install/*) — there's no CA to validate against. Only
        // skip verification for a private/loopback host, so a Machine
        // record pointed at a real public hostname still gets real
        // certificate validation.
        if ($this->machine->use_tls && $this->isPrivateHost($this->machine->host)) {
            $request = $request->withOptions(['verify' => false]);
        }

        return $request;
    }

    private function isPrivateHost(string $host): bool
    {
        if ($host === 'localhost') {
            return true;
        }

        return filter_var($host, FILTER_VALIDATE_IP) !== false
            && ! filter_var($host, FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE);
    }
}
