<?php

namespace App\Http\Controllers;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Support\AgentStream;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;
use Symfony\Component\HttpFoundation\HeaderUtils;

/**
 * Explorer's Download. A plain GET route (not a Livewire action) so the
 * browser handles the file itself — progress bar, cancel, saving to disk —
 * and the hub streams it straight through from the agent.
 */
class DownloadController extends Controller
{
    public function show(Request $request, Machine $machine)
    {
        abort_unless(Auth::user()->canAccess($machine), 403);

        $path = $request->query('path', '');
        abort_if(! is_string($path) || $path === '', 400);

        try {
            $response = $machine->agent()->download($path);
        } catch (AgentException $e) {
            abort(in_array($e->getCode(), [401, 403, 404], true) ? $e->getCode() : 502, $e->getMessage());
        } catch (ConnectionException) {
            abort(502, "{$machine->name} isn't reachable right now.");
        }

        $filename = basename(str_replace('\\', '/', $path));
        $fallback = preg_replace('/[^\x20-\x7e]|["\\\\%\/]/u', '_', $filename) ?: 'download';

        return AgentStream::respond($response, [
            'Content-Type' => 'application/octet-stream',
            'Content-Disposition' => HeaderUtils::makeDisposition(HeaderUtils::DISPOSITION_ATTACHMENT, $filename, $fallback),
            'X-Content-Type-Options' => 'nosniff',
        ]);
    }
}
