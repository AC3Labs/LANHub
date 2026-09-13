<?php

namespace App\Http\Controllers;

use App\Exceptions\AgentException;
use App\Models\Machine;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Auth;

/**
 * A plain (non-Livewire) route so an <img>/<iframe> src can point straight
 * at it — Livewire actions run over AJAX and can't back inline browser
 * rendering the way a normal GET response can. Proxies the agent's own
 * GET /api/preview response through unchanged (content-type, inline
 * disposition, and the nosniff header it sets) rather than re-deciding
 * anything about the file here.
 */
class PreviewController extends Controller
{
    public function show(Request $request, Machine $machine)
    {
        abort_unless(Auth::user()->canAccess($machine), 403);

        $path = $request->query('path', '');
        abort_if($path === '', 400);

        try {
            $response = $machine->agent()->preview($path);
        } catch (AgentException $e) {
            abort(in_array($e->getCode(), [401, 403, 404, 415], true) ? $e->getCode() : 502, $e->getMessage());
        }

        $headers = array_filter([
            'Content-Type' => $response->header('Content-Type'),
            'Content-Disposition' => $response->header('Content-Disposition'),
            'X-Content-Type-Options' => $response->header('X-Content-Type-Options'),
        ]);

        return response($response->body(), $response->status())->withHeaders($headers);
    }
}
