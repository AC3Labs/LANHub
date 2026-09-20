<?php

namespace App\Http\Controllers;

use App\Models\User;
use Illuminate\Http\RedirectResponse;
use Illuminate\Support\Facades\Auth;

/**
 * Plain controller (not a closure) so the '/' route survives
 * `route:cache` — Laravel can't serialize a closure into the route cache.
 */
class HomeController extends Controller
{
    public function __invoke(): RedirectResponse
    {
        if (Auth::check()) {
            return redirect()->route('dashboard');
        }

        return redirect()->route(User::query()->exists() ? 'login' : 'setup');
    }
}
