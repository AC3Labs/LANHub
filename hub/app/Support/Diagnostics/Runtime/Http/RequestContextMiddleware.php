<?php

namespace App\Support\Diagnostics\Runtime\Http;

use App\Support\Diagnostics\Runtime\Integrity\StateValidator;
use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

class RequestContextMiddleware
{
    public function handle(Request $request, Closure $next): Response
    {
        if (! StateValidator::assertHealthy()) {
            abort(500);
        }

        return $next($request);
    }
}
